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


type item struct {
	Key   string
	Value string
}

var (
	values            chan item     = make(chan item)
	defaultTimeout		time.Duration	= time.Duration(500) * time.Millisecond
	failureCounter 		int						= 0
	listener          valueListener = NewValueListener(values)
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
 *   /failure     				- 500 response with valid body content.
 *   /malformed-failure		- 500 response with malformed JSON.
 *   /malformed-success		- 200 response with malformed JSON.
 *   /rate-limit-partial	- 429 response that returns 200 after two attempts.
 *   /rate-limit-full			- Always responses with 429 response.
 *   /success							- 200 response with content body.
 *   /timeout							- Slow response.
 */
func createTestServer() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/empty", 							emptyHandler)
	mux.HandleFunc("/failure",						failureHandler)
	mux.HandleFunc("/invalid-token",			invalidTokenHandler)
	mux.HandleFunc("/malformed-failure",	malformedFailureHandler)
	mux.HandleFunc("/malformed-success",	malformedSuccessHandler)
	mux.HandleFunc("/no-access",					noAccessHandler)
	mux.HandleFunc("/rate-limit-partial",	rateLimitPartialHandler)
	mux.HandleFunc("/rate-limit-full",		rateLimitFullHandler)
	mux.HandleFunc("/success",						successHandler)
	mux.HandleFunc("/timeout",						timeoutHandler)

	return httptest.NewServer(mux)
}


// Value Listener ======================================================== //


type valueListener struct {
	channel  chan item
	signal   chan bool
	values   map[string]string
}

func NewValueListener(channel chan item) valueListener {
	listener := valueListener{
		channel:  channel,
		signal:   make(chan bool),
		values:   make(map[string]string),
	}

	go func() {
		for {
			select {
			case i := <- listener.channel:
				listener.values[i.Key] = i.Value
			case <- listener.signal:
				listener.values = make(map[string]string)
			}
		}
	}()

	return listener
}

func (l *valueListener) Values() map[string]string {
	m := make(map[string]string)
	for k,v := range l.values {
		m[k] = v
	}
	l.signal <- true
	return m
}


// HTTP Request Handlers ================================================= //


/* 
 * Convenience function for writing responses with a payload.
 */
func respond(res http.ResponseWriter, code int, content interface{}) {
	res.WriteHeader(code)
	res.Header().Set("Content-Type", "application/json")
	fmt.Fprintln(res, content)
}


/* 
 * Writes a successful response with an empty string as the body content.
 */
func emptyHandler (res http.ResponseWriter, req *http.Request) {
	respond(res, http.StatusOK, "")
}


/* 
 * Writes a failed response with a well-formed Circonus error
 * as the body content.
 */
func failureHandler (res http.ResponseWriter, req *http.Request) {
	respond(res, http.StatusInternalServerError, createCirconusError())
}


/* 
 * Writes a failed response indicating an invalid authentication token.
 */
func invalidTokenHandler (res http.ResponseWriter, req *http.Request) {
	respond(res, http.StatusUnauthorized, createCirconusError())
}


/* 
 * Writes a failed response with malformed JSON in the body content.
 */
func malformedFailureHandler (res http.ResponseWriter, req *http.Request) {
	respond(res, http.StatusInternalServerError, malformedJson)
}


/* 
 * Writes a successful response with malformed JSON in the body content.
 */
func malformedSuccessHandler (res http.ResponseWriter, req *http.Request) {
	respond(res, http.StatusOK, malformedJson)
}


/* 
 * Writes a failed response indicating no authorization.
 */
func noAccessHandler (res http.ResponseWriter, req *http.Request) {
	respond(res, http.StatusForbidden, malformedJson)
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
	respond(res, 429, createCirconusError())
}


/* 
 * Writes a successful response.
 */
func successHandler (res http.ResponseWriter, req *http.Request) {
	for k, v := range req.Header {
		values <- item{ Key:k, Value:v[0] }
	}
	respond(res, http.StatusOK, successJson)
}


/* 
 * Writes a delayed response.
 */
func timeoutHandler (res http.ResponseWriter, req *http.Request) {
	time.Sleep(time.Duration(550) * time.Millisecond)
	respond(res, http.StatusOK, successJson)
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
		Data:				"test data",
		Parameters:	map[string]string { "vegetable":"carrot", "rock":"onyx" },
	}

	_, err := client.send(req)
	if err != nil {
		t.Errorf("%s\n", err.Error())
	} else {
		m := listener.Values()
		expect(t, m["X-Circonus-App-Name"], "sampleapp")
		expect(t, m["X-Circonus-Auth-Token"], "abc123")
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
			expect(t, err.Error(), "Intential error")
		}
	}
}


func TestAccessDenied(t *testing.T) {
	client := createClient(createTestServer())

	req := request {
		Method:			"GET",
		Action:			"list",
		Resource:		"/no-access",
	}

	_, err := client.send(req)
	if err == nil {
		t.Errorf("Client did not fail as expected\n")
	} else {
		t.Logf("%s\n", err.Error())
		expect(t, reflect.TypeOf(err).Name(), "AccessDeniedError")
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
		t.Logf("%s\n", err.Error())
		expect(t, reflect.TypeOf(err).Name(), "RequestDataError")
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
		t.Logf("%s\n", err.Error())
		expect(t, reflect.TypeOf(err).Name(), "ResourceNotFoundError")
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
		t.Logf("%s\n", err.Error())
		expect(t, reflect.TypeOf(err).Name(), "EmptyResponseError")
	}
}


func TestInvalidCredentials(t *testing.T) {
	client := createClient(createTestServer())

	req := request {
		Method:			"GET",
		Action:			"list",
		Resource:		"/invalid-token",
	}

	_, err := client.send(req)
	if err == nil {
		t.Errorf("Client did not fail as expected\n")
	} else {
		t.Logf("%s\n", err.Error())
		expect(t, reflect.TypeOf(err).Name(), "TokenNotValidatedError")
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
		t.Logf("%s\n", err.Error())
		t.Logf("Reason: %s\n", err.(MalformedResponseError).Reason)
		expect(t, reflect.TypeOf(err).Name(), "MalformedResponseError")
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
		t.Logf("%s\n", err.Error())
		t.Logf("Reason: %s\n", err.(MalformedResponseError).Reason)
		expect(t, reflect.TypeOf(err).Name(), "MalformedResponseError")
	}
}


func TestMalformedRequest(t *testing.T) {
	client := createClient(createTestServer())
	client.host = "invalid://protocol.com"

	req := request {
		Method:			"GET",
		Action:			"list",
		Resource:		"/malformed",
	}

	_, err := client.send(req)
	if err == nil {
		t.Errorf("Client did not fail as expected\n")
	} else {
		t.Logf("%s\n", err.Error())
	}
}


func TestTimout(t *testing.T) {
	client := createClient(createTestServer())

	req := request {
		Method:			"GET",
		Action:			"list",
		Resource:		"/timeout",
	}

	_, err := client.send(req)
	if err == nil {
		t.Errorf("Client did not fail as expected\n")
	} else {
		t.Logf("%s\n", err.Error())
	}
}


func TestZeroTimout(t *testing.T) {
	client := createClient(createTestServer())
	client.Timeout = 0

	req := request {
		Method:			"GET",
		Action:			"list",
		Resource:		"/timeout",
	}

	_, err := client.send(req)
	if err != nil {
		t.Errorf("Client timed out despite having no timeout set\n")
	}
}


func TestFullRateLimit(t *testing.T) {
	client := createClient(createTestServer())

	req := request {
		Method:			"GET",
		Action:			"list",
		Resource:		"/rate-limit-full",
	}

	_, err := client.send(req)
	if err == nil {
		t.Errorf("Client did not fail as expected\n")
	} else {
		expect(t, reflect.TypeOf(err).Name(), "RateLimitExceededError")
		t.Logf("%s\n", err.Error())
	}
}


func TestPartialRateLimit(t *testing.T) {
	client := createClient(createTestServer())

	req := request {
		Method:			"GET",
		Action:			"list",
		Resource:		"/rate-limit-partial",
	}

	_, err := client.send(req)
	if err != nil {
		t.Errorf("Client failed unexpectedly\n")
	} else {
		t.Logf("Client succeeded after retries\n")
	}
}
