package circonus

import (
"fmt"
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"golang.org/x/net/context"
)

// Structures ============================================================ //

type Client struct {
	Timeout   time.Duration
	app       string          // Circonus: Application name
	host      string          // Cironus API host
	path      string          // Base URL path of any requests made
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

	clientTransport key = 0
)

// Circonus API ========================================================== //

func NewClient(appname string, apitoken string) Client {
	return Client{
		Timeout:   DEFAULT_TIMEOUT,
		app:       appname,
		host:      default_host,
		path:      "/" + supported_version,
		token:     apitoken,
		transport: &http.Transport{},
	}
}

func (c *Client) send(r request) (interface{}, error) {
	var result interface{}
	var err error

	for i := 0; i < DEFAULT_RETRY_ATTEMPTS; i++ {
		result, err = c.tryRequest(r)
		if err != nil {
			switch err.(type) {
			case RateLimitError:
				time.Sleep(DEFAULT_RETRY_INTERVAL)
				fmt.Printf("Retrying (%d)\n", i + 1)
				continue
			default:
				break
			}
		}
		fmt.Printf("Hmmm...\n")  // NOTE: We need to block on retries
	}

	return result, err
}

func (c *Client) tryRequest(r request) (interface{}, error) {
	var (
		ctx    context.Context
		cancel context.CancelFunc
		result interface{}
	)

	// Set request cancellation policy.  By default, cancellation occurs when
	// a timeout expires.  Setting a zero timeout requires manual cancellation.
	if c.Timeout.Seconds() == 0 {
		ctx, cancel = context.WithCancel(context.Background())
	} else {
		ctx, cancel = context.WithTimeout(context.Background(), c.Timeout)
	}
	defer cancel()

	// Attach transport to context
	ctx = context.WithValue(ctx, clientTransport, c.transport)

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
		return nil, err // Should only occur with malformed request URL's
	}

	// Add any querystring parameters
	if len(r.Parameters) > 0 {
		q := req.URL.Query()
		for key, value := range r.Parameters {
			q.Set(key, value)
		}
		req.URL.RawQuery = q.Encode()
	}

	// Define a response handler closure
	resHandler := func(res *http.Response, err error) error {
		if err != nil {
			return err
		}
		defer res.Body.Close()

		decoder := json.NewDecoder(res.Body)

		if res.StatusCode > 399 {
			if res.StatusCode == 404 {
				return ResourceNotFoundError{Endpoint: r.Resource}
			}
			if res.StatusCode == 429 {
				return RateLimitError{}
			}

			// Parse response as an Error
			var errorResult CirconusError
			if err := decoder.Decode(&errorResult); err != nil {
				return MalformedResponseError{Reason: err.Error()}
			} else {
				return errorResult
			}
		} else {
			// Parse successful response
			if err := decoder.Decode(&result); err != nil {
				if err.Error() == "EOF" {
					return EmptyResponseError{}
				} else {
					return MalformedResponseError{Reason: err.Error()}
				}
			} else {
				return nil
			}
		}
	}

	// Execute request
	err = executeRequest(ctx, req, resHandler)
	return result, err
}

/*
 * Runs an http.Request in a goroutine and passes the result to a handler
 * function.  Supports request cancellation via a Context object.
 */
func executeRequest(ctx context.Context, req *http.Request, fn responseHandler) error {
	tr := ctx.Value(clientTransport).(*http.Transport)
	client := &http.Client{Transport: tr}
	echannel := make(chan error, 1)

	go func() {
		echannel <- fn(client.Do(req))
	}()

	select {
	case <-ctx.Done():
		tr.CancelRequest(req)
		<-echannel // Block and wait for fn to return
		return ctx.Err()
	case err := <-echannel:
		return err
	}
}

// Convenience Functions ================================================= //

// DEBUG
func (c *Client) List(resource string) (interface{}, error) {
	req := request{
		Method:   "GET",
		Action:   "list",
		Resource: resource,
	}
	return c.send(req)
}
