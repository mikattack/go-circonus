package circonus

import (
//"fmt"
"io"
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"golang.org/x/net/context"
)


type Client struct {
	Timeout		time.Duration
	app				string					// Circonus: Application name
	host			string					// Cironus API host
	path			string					// Base URL path of any requests made
	token			string					// Circonus: API token
	transport	*http.Transport	// For testing
}

type CirconusError struct {
	Code					string		`json:"code"`
	Explanation		string		`json:"explanation"`
	Message				string		`json:"message"`
	Reference			string		`json:"reference"`
	Tag						string		`json:"tag"`
	Server				string		`json:"server"`
}

type request struct {
	Method			string
	Action			string
	Resource		string
	Data				interface{}
	Parameters	map[string]string
}

type key int
type responseHandler func(*http.Response, error) error


// External constants
var (
	DEFAULT_TIMEOUT time.Duration = time.Duration(30) * time.Second

	// Supported resources
	ACCOUNT					string = "account"
	BROKER					string = "broker"
	CHECK						string = "check"
	CHECK_BUNDLE		string = "checkbundle"
	CONTACT_GROUP		string = "contact_group"
	GRAPH						string = "graph"
	RULE_SET				string = "rule_set"
	RULE_SET_GROUP	string = "rule_set_group"
	TEMPLATE				string = "template"
	USER						string = "user"

)

// Internal constants
var (
	default_host			string = "https://api.circonus.com"
	supported_version string = "v2"
	retries						int = 5

	clientTransport		key = 0
)


func (e CirconusError) Error() string {
	return e.Explanation
}


func NewClient(appname string, apitoken string) Client {
	return Client {
		Timeout:		DEFAULT_TIMEOUT,
		app:				appname,
		host:				default_host,
		path:				"/" + supported_version,
		token:			apitoken,
		transport:	&http.Transport { },
	}
}


// DEBUG
func (c *Client) List(resource string) (interface{}, error) {
	req := request {
		Method:			"GET",
		Action:			"list",
		Resource:		resource,
	}
	return c.send(req)
}


func (c *Client) send(r request) (interface{}, error) {
	var (
		ctx			context.Context
		cancel	context.CancelFunc
		result  interface{}
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
			return nil, errors.New("Cannot encode request data: " + err.Error())
		} else {
			encoded_data = bytes.NewBuffer(encoded)
		}
	}

	// Create request
	url := c.host + c.path + r.Resource
	///////////////////fmt.Printf("URL: %s\n", url)
	req, err := http.NewRequest(r.Method, url, encoded_data)
	if err != nil {
		return nil, err		// Should only occur with malformed request URL's
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

		// REMOVE THIS DEBUG CODE ///////////////////////////////////////////////
		buffer := new(bytes.Buffer)
	  if _, err := io.Copy(buffer, res.Body); err != nil {
	    return errors.New("Failed to buffer the request body")
	  }
	  decoder := json.NewDecoder(bytes.NewReader(buffer.Bytes()))
	  /////////////////////////////////////////////////////////////////////////

		//decoder := json.NewDecoder(res.Body)

		///////////////////fmt.Printf("CODE: %d\n", res.StatusCode)
		///////////////////fmt.Printf("STATUS: %s\n", res.Status)
		if res.StatusCode > 399 {
			if res.StatusCode == 404 {
				return errors.New("Circonus endpoint \"" + r.Resource + "\" not found")
			}

			// Parse response as an Error
			var errorResult CirconusError
			if err := decoder.Decode(&errorResult); err != nil {
				return errors.New(buffer.String())
				//return errors.New("Failed to decode server response")
			} else {
				return errorResult
			}
		} else {
			// Parse successful response
			if err := decoder.Decode(&result); err != nil {
				if err.Error() == "EOF" {
					return errors.New("Empty response from Circonus")
				} else {
					return errors.New("Malformed response from Circonus")
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
	client := &http.Client { Transport:tr }
	echannel := make(chan error, 1)

	go func() {
		echannel <- fn(client.Do(req))
	}()

	select {
	case <-ctx.Done():
		tr.CancelRequest(req)
		<- echannel		// Block and wait for fn to return
		return ctx.Err()
	case err := <- echannel:
		return err
	}
}
