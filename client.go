package circonus

import (
"fmt"
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	//"golang.org/x/net/context"
)

// Structures ============================================================ //

type Client struct {
	Retries   int
	Timeout   time.Duration
	app       string          // Circonus: Application name
	host      string          // Cironus API host
	path      string          // Base URL path of any requests made
	results   chan result			// Used to limit client to a one request at a time
	token     string          // Circonus: API token
	transport *http.Transport // For testing
}

type request struct {
	Method     string
	Action     string
	Resource   string
	Data       interface{}
	Parameters map[string]string
}

type result struct {
	Response	interface{}
	Error			error
}

type key int
type resource string
type responseHandler func(*http.Response, error) error

// Errors ================================================================ //

type CirconusError struct {
	Code        string `json:"code"`
	Explanation string `json:"explanation"`
	Message     string `json:"message"`
	Reference   string `json:"reference"`
	Tag         string `json:"tag"`
	Server      string `json:"server"`
}

func (e CirconusError) Error() string {
	return e.Explanation
}

type EmptyResponseError struct{}

func (e EmptyResponseError) Error() string {
	return "Empty response from Circonus"
}

type MalformedResponseError struct {
	Reason string
}

func (e MalformedResponseError) Error() string {
	return "Malformed JSON response from Circonus"
}

type RateLimitError struct {}

func (e RateLimitError) Error() string {
	return "Request was rate limited"
}

type RateLimitExceededError struct {}

func (e RateLimitExceededError) Error() string {
	return "Request exceeded rate limit and exhausted retries"
}

type RequestDataError struct {
	Reason string
}

func (e RequestDataError) Error() string {
	return "Cannot encode request data"
}

type ResourceNotFoundError struct {
	Endpoint string
}

func (e ResourceNotFoundError) Error() string {
	return "Circonus endpoint \"" + e.Endpoint + "\" not found"
}

// Constants & Data ====================================================== //

// External constants
const (
	DEFAULT_TIMEOUT        time.Duration = time.Duration(30) * time.Second
	DEFAULT_RETRY_ATTEMPTS int = 5
	DEFAULT_RETRY_INTERVAL time.Duration = time.Duration(1) * time.Second

	// Supported resources
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

// Internal constants
var (
	default_host      string = "https://api.circonus.com"
	supported_version string = "v2"
	retries           int    = 5

	clientTransport   key = 0
)

// Circonus API ========================================================== //

func NewClient(appname string, apitoken string) Client {
	return Client{
		Retries:   DEFAULT_RETRY_ATTEMPTS,
		Timeout:   DEFAULT_TIMEOUT,
		app:       appname,
		host:      default_host,
		path:      "/" + supported_version,
		results:   make(chan result),
		token:     apitoken,
		transport: &http.Transport{},
	}
}

func (c *Client) send(r request) (interface{}, error) {
	var res interface{}
	var err error

	go func(req request, channel chan result) {
		for i := 0; i < c.Retries; i++ {
			res, err = c.tryRequest(r, c.results)

			if err != nil {
				switch err.(type) {
				case RateLimitError:
					<- time.Tick(DEFAULT_RETRY_INTERVAL)
					fmt.Printf("Retrying (%d)\n", i + 1)
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

	// Add any querystring parameters
	if len(r.Parameters) > 0 {
		q := req.URL.Query()
		for key, value := range r.Parameters {
			q.Set(key, value)
		}
		req.URL.RawQuery = q.Encode()
	}

	client := &http.Client{
		Timeout:   c.Timeout,
		Transport: c.transport,
	}

	// Execute request
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	decoder := json.NewDecoder(res.Body)

	// Handle errors
	if res.StatusCode > 399 {
		if res.StatusCode == 404 {
			return nil, ResourceNotFoundError{Endpoint: r.Resource}
		}
		if res.StatusCode == 429 {
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

// Convenience Functions ================================================= //

/*
func (c *Client) List(resource string) (interface{}, error) {
	req := request{
		Method:   "GET",
		Action:   "list",
		Resource: resource,
	}
	return c.send(req)
}
*/
