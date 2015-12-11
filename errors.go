package circonus


type AccessDeniedError struct {}

func (e AccessDeniedError) Error() string {
  return "Access denied"
}

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

type TokenNotValidatedError struct {}

func (e TokenNotValidatedError) Error() string {
  return "Invalid authentication token"
}
