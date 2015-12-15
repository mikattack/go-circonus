package circonus


func (c *Client) Add(resource string, data interface{}, params map[string]string) (interface{}, error) {
	req := request{
		Method:     "POST",
		Resource:   resource,
		Data:       data,
		Parameters: params,
	}
	return c.send(req)
}

func (c *Client) Delete(resource string, id string, data interface{}) (interface{}, error) {
	req := request{
		Method:     "DELETE",
		Resource:   resource + "/" + id,
		Data:       data,
	}
	return c.send(req)
}

func (c *Client) Edit(resource string, id string, data interface{}) (interface{}, error) {
	req := request{
		Method:     "PUT",
		Resource:   resource + "/" + id,
		Data:       data,
	}
	return c.send(req)
}

func (c *Client) Get(resource string, id string, data interface{}) (interface{}, error) {
	req := request{
		Method:     "Get",
		Resource:   resource + "/" + id,
		Data:       data,
	}
	return c.send(req)
}

func (c *Client) List(resource string, data interface{}) (interface{}, error) {
	req := request{
		Method:     "GET",
		Resource:   resource,
	}
	return c.send(req)
}
