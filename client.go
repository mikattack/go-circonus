package circonus

import (
//"fmt"
	"bytes"
	"encoding/json"
	"net/http"
	"time"
)

// Structures ============================================================ //

// A Client is a Circonus client.  Its zero value is a usable client with
// default timeouts and retries.
// 
// Although Clients should be safe for concurrent use, in practice doing so
// will provide little benefit as Circonus rate limits use by access token.
// 
// Clients typically maintain internal (cached) state and so should be reused 
// rather than created as needed.
type Client struct {
	// Retries specifies the number of times the Client will retry a request
	// that has failed because of rate limiting.  After the maximum number of
	// retries has been attempted, a RateLimitExceededError will be returned.
	// 
	// The default value is five attempts.
	Retries int

	// Timeout specifies a time limit for requests made by the Client.  This
	// includes connection time and reading the response.  The timer will
	// interrupt request processing when exceeded, cancelling the request.
	//
	// A Timeout of zero means no timeout.
	// 
	// The default value is 30 seconds.
	Timeout time.Duration

	app         string          // Circonus: Application name
	host        string          // Cironus API host
	httpclient  *http.Client
	path        string          // Base URL path of any requests made
	results     chan result     // Used to limit client to a one request at a time
	token       string          // Circonus: API token
	transport   *http.Transport // For testing
}

// Internal type for encapsulating requests to send to Circonus.
type request struct {
	Method     string
	Resource   string
	Data       interface{}
	Parameters map[string]string
}

// Internal type for representing a response from Circonus.
type result struct {
	Response	interface{}
	Error     error
}

// Internal type for representing valid Circonus endpoints.
type resource string

// Constants & Data ====================================================== //

// Resource endpoint designators for use with convenience functions.
const (
	ACCOUNT        resource = "account"
	BROKER         resource = "broker"
	CHECK          resource = "check"
	CHECK_BUNDLE   resource = "checkbundle"
	CONTACT_GROUP  resource = "contact_group"
	GRAPH          resource = "graph"
	RULE_SET       resource = "rule_set"
	RULE_SET_GROUP resource = "rule_set_group"
	TEMPLATE       resource = "template"
	USER           resource = "user"
)

const (
	default_host           string = "https://api.circonus.com"
	default_retry_attempts int = 5
	default_retry_interval time.Duration = time.Duration(1) * time.Second
	default_timeout        time.Duration = time.Duration(30) * time.Second
	supported_version      string = "v2"
)

// Client API ============================================================ //

// Creates a new Client for use with Circonus account matching the given
// application identifier and account access token.
func NewClient(appname string, apitoken string) Client {
	return Client{
		Retries:   default_retry_attempts,
		Timeout:   default_timeout,
		app:       appname,
		host:      default_host,
		path:      "/" + supported_version,
		results:   make(chan result),
		token:     apitoken,
		transport: &http.Transport{},
	}
}

// Send a request to Circonus and return the response it returns.
// 
// If Circonus throttles a request because of rate limiting, it will be
// retried until it succeeds, errors, or exceeds the configured number of
// retry attempts.
func (c *Client) send(r request) (interface{}, error) {
	var res interface{}
	var err error

	if c.httpclient == nil {
    c.httpclient = &http.Client{
      Timeout:   c.Timeout,
      Transport: c.transport,
    }
  }

	go func(req request, channel chan result) {
		for i := 0; i < c.Retries; i++ {
			res, err = c.tryRequest(r, c.results)

			if err != nil {
				switch err.(type) {
				case RateLimitError:
					<- time.Tick(default_retry_interval)
					if i == c.Retries - 1 {
						err = RateLimitExceededError{}
					}
					continue
				default:
					break  // Stop on general errors
				}
			}

			break  // Stop immediately upon success
		}
		channel <- result{
			Response: res,
			Error:    err,
		}
	}(r, c.results)

	// Await successful response or maximum retries
	for {
		select {
			case res := <- c.results:
				return res.Response, res.Error
		}
	}
}

// Attempts to send a single request to Circonus, process its response, and
// return the results over a given channel.
func (c *Client) tryRequest(r request, channel chan result) (interface{}, error) {
	var response interface{}

	// Encode data as JSON
	encoded_data := new(bytes.Buffer)
	if r.Data != nil {
		if encoded, err := json.Marshal(r.Data); err != nil {
			return nil, RequestDataError{Reason: err.Error()}
		} else {
			encoded_data = bytes.NewBuffer(encoded)
		}
	}

	// Create request
	url := c.host + c.path + r.Resource
	req, err := http.NewRequest(r.Method, url, encoded_data)
	if err != nil {
		return nil, err  // Should only occur with malformed request URL's
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Circonus-App-Name", c.app)
	req.Header.Set("X-Circonus-Auth-Token", c.token)

	// Add any querystring parameters
	if len(r.Parameters) > 0 {
		q := req.URL.Query()
		for key, value := range r.Parameters {
			q.Set(key, value)
		}
		req.URL.RawQuery = q.Encode()
	}

	// Execute request
	res, err := c.httpclient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	decoder := json.NewDecoder(res.Body)

	// Handle errors
	if res.StatusCode > 399 {
		switch res.StatusCode {
		case 401:
			return nil, TokenNotValidatedError{}
		case 403:
			return nil, AccessDeniedError{}
		case 404:
			return nil, ResourceNotFoundError{Endpoint: r.Resource}
		case 429:
			return nil, RateLimitError{}
		}

		// Parse response as an Error
		var errorResult CirconusError
		if err := decoder.Decode(&errorResult); err != nil {
			return nil, MalformedResponseError{Reason: err.Error()}
		} else {
			return nil, errorResult
		}
	}

	// Parse successful response
	if err := decoder.Decode(&response); err != nil {
		if err.Error() == "EOF" {
			return nil, EmptyResponseError{}
		} else {
			return nil, MalformedResponseError{Reason: err.Error()}
		}
	}

	return response, nil
}
