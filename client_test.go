package circonus

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"
)


var (
	defaultTimeout		time.Duration	= time.Duration(3) * time.Second
	failureCounter 		int						= 0
	malformedJson 		string				= "{ count:4 )"
	successJson 			string				= "{ \"data\":[1,2,3,4] }"
)


func expect(t *testing.T, a interface{}, b interface{}) {
	if a != b {
		t.Errorf("Expected \"%v\" (%s), got \"%v\" (%s)", b, reflect.TypeOf(b), a, reflect.TypeOf(a))
	}
}


// Factory Functions ===================================================== //


/* 
 * Creates a complete Circonus error message, encoded as JSON.
 */
func createCirconusError() string {
	var cerror []byte
	ce := CirconusError {
		Code:					"1234",
		Explanation:	"Intential error",
		Message:			"Test-triggered error",
		Reference:		"code-1234",
		Tag:					"id-abcd",
		Server:				"test",
	}
	cerror, err := json.Marshal(ce)
	if err != nil {
		fmt.Errorf("Bad factory function: createCirconusError()\n")
	}
	return string(cerror)
}


/* 
 * Creates a Client configured for testing.
 * 
 * Arguments:
 *		server	Server Client requests will be proxied to.
 */
func createClient(server *httptest.Server) Client {
	serverURL, err := url.Parse(server.URL)

	tr := &http.Transport {
		Proxy:	func(req *http.Request) (*url.URL, error) {
			return serverURL, err
		},
	}

	client := NewClient("sampleapp", "abc123")
	client.Timeout = defaultTimeout
	client.host = serverURL.String()
	client.path = ""
	client.transport = tr
	return client
}


/* 
 * Creates an HTTP server listening on the local loopback interface, for use
 * in end-to-end testing.  This server mocks the Circonus service.
 * 
 * The following resources are exposed:
 * 
 *   /empty								- Empty server response.
 *   /malformed-failure		- 500 response with malformed JSON.
 *   /malformed-success		- 200 response with malformed JSON.
 *   /rate-limit-partial	- 429 response that returns 200 after two attempts.
 *   /rate-limit-full			- Always responses with 429 response.
 *   /server-error				- 500 response with valid body content.
 *   /success							- 200 response with content body.
 *   /timeout							- Slow response.
 */
func createTestServer() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/empty", 							emptyHandler)
	mux.HandleFunc("/failure",						failureHandler)
	mux.HandleFunc("/malformed-failure",	malformedFailureHandler)
	mux.HandleFunc("/malformed-success",	malformedSuccessHandler)
	mux.HandleFunc("/rate-limit-partial",	rateLimitPartialHandler)
	mux.HandleFunc("/rate-limit-full",		rateLimitFullHandler)
	mux.HandleFunc("/server-error",				serverErrorHandler)
	mux.HandleFunc("/success",						successHandler)
	mux.HandleFunc("/timeout",						timeoutHandler)

	return httptest.NewServer(mux)
}


// HTTP Request Handlers ================================================= //


/* 
 * Writes a successful response with an empty string as the body content.
 */
func emptyHandler (res http.ResponseWriter, req *http.Request) {
	res.WriteHeader(http.StatusOK)
	res.Header().Set("Content-Type", "application/json")
	fmt.Fprintln(res, "")
}


/* 
 * Writes a failed response with a well-formed Circonus error
 * as the body content.
 */
func failureHandler (res http.ResponseWriter, req *http.Request) {
	res.WriteHeader(http.StatusInternalServerError)
	res.Header().Set("Content-Type", "application/json")
	fmt.Fprintln(res, createCirconusError())
}


/* 
 * Writes a failed response with malformed JSON in the body content.
 */
func malformedFailureHandler (res http.ResponseWriter, req *http.Request) {
	res.WriteHeader(http.StatusInternalServerError)
	res.Header().Set("Content-Type", "application/json")
	fmt.Fprintln(res, malformedJson)
}


/* 
 * Writes a successful response with malformed JSON in the body content.
 */
func malformedSuccessHandler (res http.ResponseWriter, req *http.Request) {
	res.WriteHeader(http.StatusOK)
	res.Header().Set("Content-Type", "application/json")
	fmt.Fprintln(res, malformedJson)
}


/* 
 * Writes an error response with a rate-limiting status code.
 * 
 * After two failure responses, a single successful response will be written.
 * The pattern of two failed, one successful, will be repeated thereafter.
 */
func rateLimitPartialHandler (res http.ResponseWriter, req *http.Request) {
	if failureCounter < 2 {
		// Rate limit error
		res.WriteHeader(429)
		res.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(res, createCirconusError())		// Client ignores error message
		failureCounter += 1
	} else {
		// Successful response
		res.WriteHeader(http.StatusOK)
		res.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(res, successJson)
		failureCounter = 0
	}
}


/* 
 * Writes an error response with a rate-limiting status code.
 */
func rateLimitFullHandler (res http.ResponseWriter, req *http.Request) {
	res.WriteHeader(429)
	res.Header().Set("Content-Type", "application/json")
	fmt.Fprintln(res, createCirconusError())		// Client ignores error message
}


/* 
 * Writes a failed response.
 */
func serverErrorHandler (res http.ResponseWriter, req *http.Request) {
	res.WriteHeader(http.StatusInternalServerError)
	res.Header().Set("Content-Type", "application/json")
	fmt.Fprintln(res, createCirconusError())
}


/* 
 * Writes a successful response.
 */
func successHandler (res http.ResponseWriter, req *http.Request) {
	res.WriteHeader(http.StatusOK)
	res.Header().Set("Content-Type", "application/json")
	fmt.Fprintln(res, successJson)
}


/* 
 * Writes a delayed response.
 */
func timeoutHandler (res http.ResponseWriter, req *http.Request) {
	time.Sleep(time.Duration(3) * time.Second)
	res.WriteHeader(http.StatusOK)
	res.Header().Set("Content-Type", "application/json")
	fmt.Fprintln(res, successJson)
}


// Tests ================================================================= //


/* 
 * NOTE:
 *		All methods of the client behave the same way, so we only need
 *		to test behavior and edge cases that affect all request types.
 */


func TestSuccess(t *testing.T) {
	client := createClient(createTestServer())

	req := request {
		Method:			"GET",
		Action:			"list",
		Resource:		"/success",
	}

	_, err := client.send(req)
	if err != nil {
		t.Errorf("%s\n", err.Error())
	}
}


func TestFailure(t *testing.T) {
	client := createClient(createTestServer())

	req := request {
		Method:			"GET",
		Action:			"list",
		Resource:		"/failure",
	}

	_, err := client.send(req)
	if err == nil {
		t.Errorf("Client did not fail as expected\n")
	} else {
		decoded, jerr := json.Marshal(err)
		if jerr != nil {
			t.Errorf("Client error could not be serialized to JSON\n")
		} else {
			expect(t, string(decoded), createCirconusError())
		}
	}
}


func TestBadRequestData(t *testing.T) {
	client := createClient(createTestServer())

	req := request {
		Method:			"GET",
		Action:			"list",
		Resource:		"/success",
		Data:				make(chan bool),
	}

	_, err := client.send(req)
	if err == nil {
		t.Errorf("Client did not fail as expected\n")
	} else {
		t.Logf("%v\n", err)
	}
}


func TestBadResource(t *testing.T) {
	client := createClient(createTestServer())

	req := request {
		Method:			"GET",
		Action:			"list",
		Resource:		"/nonexistent",
	}

	_, err := client.send(req)
	if err == nil {
		t.Errorf("Client did not fail as expected\n")
	} else {
		t.Logf("%v\n", err)
	}
}


func TestEmptyResponse(t *testing.T) {
	client := createClient(createTestServer())

	req := request {
		Method:			"GET",
		Action:			"list",
		Resource:		"/empty",
	}

	_, err := client.send(req)
	if err == nil {
		t.Errorf("Client did not fail as expected\n")
	} else {
		t.Logf("%v\n", err)
	}
}


func TestMalformedSuccess(t *testing.T) {
	client := createClient(createTestServer())

	req := request {
		Method:			"GET",
		Action:			"list",
		Resource:		"/malformed-success",
	}

	_, err := client.send(req)
	if err == nil {
		t.Errorf("Client did not fail as expected\n")
	} else {
		t.Logf("%v\n", err)
		expect(t, err.Error(), "Malformed response from Circonus")
	}
}


func TestMalformedFailure(t *testing.T) {
	client := createClient(createTestServer())

	req := request {
		Method:			"GET",
		Action:			"list",
		Resource:		"/malformed-failure",
	}

	_, err := client.send(req)
	if err == nil {
		t.Errorf("Client did not fail as expected\n")
	} else {
		t.Logf("%v\n", err)
	}
}

/*
 * TESTS:
 * 
 * NOTE: 	Tests should not check error strings.  Add error objects to the
 *				client code.
 * 
 * send()
 *	- unencodable request data
 *  - bad request URL
 *	- non-2XX server response
 *  - empty server response
 *  - malformed JSON response to failed request
 *  - malformed JSON response to successful request
 *  - timeout response
 *  - rate limiting response: twice 429, then success
 *  - rate limiting response: always 429
 */
