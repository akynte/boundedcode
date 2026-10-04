package client

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
)

const base = "http://payments:8080"

type Config struct {
	BaseURL string `env:"PAYMENTS_URL"`
	Timeout int    `envconfig:"PAYMENTS_TIMEOUT"`
}

type Client struct {
	c       *http.Client
	baseURL string
}

func (c *Client) Create(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/payments", strings.NewReader("{}"))
	if err != nil {
		return err
	}
	_, err = c.c.Do(req)
	return err
}

func (c *Client) Get(ctx context.Context, id string) error {
	_, err := http.NewRequest("GET", fmt.Sprintf("%s/v1/payments/%s", base, id), nil)
	return err
}

func (c *Client) Dynamic(id string) {
	_, _ = http.NewRequest("GET", c.baseURL+"/v2/orders/"+id, nil)
	_, _ = c.c.Get("https://inventory.internal/stock")
	_ = os.Getenv("INVENTORY_TOKEN_FILE")
	if v, ok := os.LookupEnv("REGION"); ok {
		_ = v
	}
}
