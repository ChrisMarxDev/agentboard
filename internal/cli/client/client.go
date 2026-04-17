package client

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
)

// Client communicates with a running AgentBoard server.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// New creates a new CLI client.
func New(baseURL string) *Client {
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{},
	}
}

func (c *Client) do(method, path string, body []byte) ([]byte, error) {
	url := c.baseURL + path

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-Source", "cli")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not connect to server at %s: %w", c.baseURL, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("server error (%d): %s", resp.StatusCode, string(data))
	}

	return data, nil
}

func (c *Client) Set(key string, value []byte) error {
	_, err := c.do("PUT", "/api/data/"+key, value)
	if err != nil {
		return err
	}
	fmt.Println("OK")
	return nil
}

func (c *Client) Get(key string) ([]byte, error) {
	data, err := c.do("GET", "/api/data/"+key, nil)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (c *Client) List(prefix string) ([]byte, error) {
	path := "/api/data"
	if prefix != "" {
		path += "?prefix=" + prefix
	}
	return c.do("GET", path, nil)
}

func (c *Client) Merge(key string, value []byte) error {
	_, err := c.do("PATCH", "/api/data/"+key, value)
	if err != nil {
		return err
	}
	fmt.Println("OK")
	return nil
}

func (c *Client) Append(key string, value []byte) error {
	_, err := c.do("POST", "/api/data/"+key, value)
	if err != nil {
		return err
	}
	fmt.Println("OK")
	return nil
}

func (c *Client) Delete(key string) error {
	_, err := c.do("DELETE", "/api/data/"+key, nil)
	if err != nil {
		return err
	}
	fmt.Println("OK")
	return nil
}

func (c *Client) DeleteById(key, id string) error {
	_, err := c.do("DELETE", "/api/data/"+key+"/"+id, nil)
	if err != nil {
		return err
	}
	fmt.Println("OK")
	return nil
}

func (c *Client) Schema() ([]byte, error) {
	return c.do("GET", "/api/data/schema", nil)
}
