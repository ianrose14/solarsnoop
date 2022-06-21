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
	appKey string // equivalent to API key, the docs use these interchangeably
}

func NewClient(appKey string) *Client {
	return &Client{appKey: appKey}
}

type OAuthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

// TODO(ianrose): check for vacation (or hold?) events - maybe we can differentiate manual vs programmatic hold events by always setting the end time to some specific # of seconds

func (c *Client) CompleteAuthorization(ctx context.Context, authCode, redirectUrl string) (*OAuthTokenResponse, error) {
	const urlstr = "https://api.ecobee.com/token"

	qs := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {authCode},
		"redirect_uri": {redirectUrl},
		"client_id":    {c.appKey},
		"ecobee_type":  {"jwt"},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", urlstr+"?"+qs.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to make new http request: %w", err)
	}

	rsp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST failure: %w", err)
	}
	defer drainAndClose(rsp.Body)

	if rsp.StatusCode >= 300 {
		err := httpResponseError(rsp)
		return nil, fmt.Errorf("POST failure: %w", err)
	}

	var body OAuthTokenResponse
	if err := json.NewDecoder(rsp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("failed to parse response body: %w", err)
	}

	return &body, nil
}

func (c *Client) postFunction(_ context.Context, accessToken string, funcType string, params map[string]any) error {
	const urlstr = "https://api.ecobee.com/1/thermostat?format=json"

	type thermoFunc struct {
		Type   string         `json:"type"`
		Params map[string]any `json:"params"`
	}

	var body struct {
		Selection struct {
			SelectionType string `json:"selectionType"`
		} `json:"selection"`
		Functions []thermoFunc `json:"functions"`
	}

	body.Selection.SelectionType = "registered"
	body.Functions = []thermoFunc{
		{Type: funcType, Params: params},
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(&body); err != nil {
		return fmt.Errorf("failed to json-encode body: %w", err)
	}

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

	return nil
}

func (c *Client) RefreshTokens(ctx context.Context, refreshToken string) (*OAuthTokenResponse, error) {
	const urlstr = "https://api.ecobee.com/token"

	qs := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {c.appKey},
		"ecobee_type":   {"jwt"},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", urlstr+qs.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to make new http request: %w", err)
	}

	rsp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST failure: %w", err)
	}
	defer drainAndClose(rsp.Body)

	if rsp.StatusCode >= 300 {
		err := httpResponseError(rsp)
		return nil, fmt.Errorf("POST failure: %w", err)
	}

	var body OAuthTokenResponse
	if err := json.NewDecoder(rsp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("failed to parse response body: %w", err)
	}

	return &body, nil
}

func (c *Client) SendMessage(ctx context.Context, accessToken, text string) error {
	return c.postFunction(ctx, accessToken, "sendMessage", map[string]any{"text": text})
}

// SetHold sets a temperature hold (in degress fahrenheit) for a fixed number of hours.  The start time is always the
// current time.
func (c *Client) SetHold(ctx context.Context, accessToken string, coldHoldTemp, heatHoldTemp, holdHours int) error {
	return c.postFunction(ctx, accessToken, "setHold", map[string]any{
		"holdType":     "holdHours",
		"heatHoldTemp": heatHoldTemp * 10,
		"coldHoldTemp": coldHoldTemp * 10,
		"holdHours":    holdHours,
	})
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
