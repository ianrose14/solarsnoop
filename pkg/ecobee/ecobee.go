package ecobee

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
)

type Client struct {
}

func NewClient() *Client {
	return &Client{}
}

func (c *Client) CompleteAuthorization(ctx context.Context, authCode, redirectUrl, appKey string) (accessToken string, refreshToken string, e error) {
	const urlstr = "https://api.ecobee.com/token"

	qs := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {authCode},
		"redirect_uri": {redirectUrl},
		"client_id":    {appKey},
		"ecobee_type":  {"jwt"},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", urlstr+"?"+qs.Encode(), nil)
	if err != nil {
		return "", "", fmt.Errorf("failed to make new http request: %w", err)
	}

	rsp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("POST failure: %w", err)
	}
	defer drainAndClose(rsp.Body)

	if rsp.StatusCode >= 300 {
		err := httpResponseError(rsp)
		return "", "", fmt.Errorf("POST failure: %w", err)
	}

	var body struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
		Scope        string `json:"scope"`
	}
	if err := json.NewDecoder(rsp.Body).Decode(&body); err != nil {
		return "", "", fmt.Errorf("failed to parse response body: %w", err)
	}

	return body.AccessToken, body.RefreshToken, nil
}

func (c *Client) SetHold(ctx context.Context, accessToken string, coldHoldTemp, heatHoldTemp, holdHours int) error {
	const urlstr = "https://api.ecobee.com/1/thermostat?format=json"

	type ecobeeFunction struct {
		Type string
		Params map[string]string

	}

	body := struct {
		Function []struct
		Selection "selectionType":"registered",
			"selectionMatch":""
		},
	}

	body := x
	var buf bytes.Buffer

	req, err := http.NewRequest("POST", urlstr, &buf)
	if err != nil {
		return fmt.Errorf("failed to create http request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	rsp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST failure: %w", err)
	}
	defer drainAndClose(rsp.Body)

	if rsp.StatusCode >= 300 {
		err := httpResponseError(rsp)
		return fmt.Errorf("POST failure: %w", err)
	}
}

func drainAndClose(rc io.ReadCloser) {
	_, _ = ioutil.ReadAll(rc)
	_ = rc.Close()
}

func httpResponseError(rsp *http.Response) error {
	body, err := ioutil.ReadAll(rsp.Body)
	if err != nil {
		body = []byte(fmt.Sprintf("failed to read body (%s)", err))
	}
	return fmt.Errorf("POST failure (%s): %s", rsp.Status, truncateString(string(body), 500))
}

func truncateString(s string, maxlen int) string {
	if len(s) <= maxlen {
		return s
	}
	return s[:maxlen]
}
