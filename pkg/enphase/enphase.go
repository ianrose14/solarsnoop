package enphase

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type OAuthTokenResponse struct {
	AccessToken         string `json:"access_token"`
	TokenType           string `json:"token_type"`
	RefreshToken        string `json:"refresh_token"`
	ExpiresIn           int64  `json:"expires_in"` // seconds
	Scope               string `json:"scope"`
	UserId              string `json:"enl_uid"`
	CID                 string `json:"enl_cid"`                   // what is this?
	PasswordLastChanged string `json:"enl_password_last_changed"` // unix timestamp
	IsInternalApp       bool   `json:"is_internal_app"`
	AppType             string `json:"app_type"`
	JTI                 string `json:"jti"`
}

type System struct {
	SystemId   int64 `json:"system_id"`
	Name       string
	PublicName string `json:"public_name"`
	Timezone   string
	Address    struct {
		State      string
		Country    string
		PostalCode string `json:"postal_code"`
	}
	ConnectionType string `json:"connection_type"` // e.g. "ethernet"
	Status         string // e.g. "micro"
	LastReportAt   int64  `json:"last_report_at"` // unix timestamp (seconds)
	LastEnergyAt   int64  `json:"last_energy_at"` // unix timestamp (seconds)
	OperationalAt  int64  `json:"operational_at"` // unix timestamp (seconds)
}

type Client struct {
	apiKey       string
	clientId     string
	clientSecret string
}

func NewClient(apiKey, clientId, clientSecret string) *Client {
	return &Client{
		apiKey:       apiKey,
		clientId:     clientId,
		clientSecret: clientSecret,
	}
}

func (c *Client) CompleteAuthorization(ctx context.Context, authCode, redirectUrl string) (*OAuthTokenResponse, error) {
	const urlstr = "https://api.enphaseenergy.com/oauth/token"

	formData := url.Values{
		"URL":          {urlstr},
		"grant_type":   {"authorization_code"},
		"redirect_uri": {redirectUrl},
		"code":         {authCode},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", urlstr, strings.NewReader(formData.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to make new http request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(c.clientId, c.clientSecret)

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

func (c *Client) FetchConsumption(ctx context.Context, systemId int64, accessToken string, startAt time.Time) (int64, error) {
	qs := url.Values{
		"start_at":    {strconv.FormatInt(startAt.Unix(), 10)},
		"granularity": {"15mins"},
	}
	urlstr := fmt.Sprintf("https://api.enphaseenergy.com/api/v4/systems/%d/telemetry/consumption_meter?%s",
		systemId, qs.Encode())

	req, err := http.NewRequestWithContext(ctx, "GET", urlstr, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to make new http request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("key", c.apiKey)

	rsp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("GET failure: %w", err)
	}

	defer drainAndClose(rsp.Body)

	if rsp.StatusCode >= 300 {
		err := httpResponseError(rsp)
		return 0, fmt.Errorf("POST failure: %w", err)
	}

	var body struct {
		StartAt   int64 `json:"start_at"`
		Intervals []struct {
			EndAt            int64 `json:"end_at"`
			DevicesReporting int   `json:"devices_reporting"`
			EnWh             int64 `json:"enwh"` // "units produced per interval" (wat?)
		}
	}

	if err := json.NewDecoder(rsp.Body).Decode(&body); err != nil {
		return 0, fmt.Errorf("failed to parse response body: %w", err)
	}

	// TODO(ianrose): should we only be taking 1 interval?  (based on EndAt?)
	var watts int64
	for _, interval := range body.Intervals {
		log.Printf("interval: %+v", interval) // NOSUBMIT
		// multiple by 4 because X Watt*hours over 15 minutes is 4*X watts
		watts += interval.EnWh * 4
	}

	return watts, nil
}

func (c *Client) FetchProduction(ctx context.Context, systemId int64, accessToken string, startAt time.Time) (int64, error) {
	qs := url.Values{
		"start_at":    {strconv.FormatInt(startAt.Unix(), 10)},
		"granularity": {"15mins"},
	}
	urlstr := fmt.Sprintf("https://api.enphaseenergy.com/api/v4/systems/%d/telemetry/production_meter?%s",
		systemId, qs.Encode())

	req, err := http.NewRequestWithContext(ctx, "GET", urlstr, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to make new http request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("key", c.apiKey)

	rsp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("GET failure: %w", err)
	}

	defer drainAndClose(rsp.Body)

	if rsp.StatusCode >= 300 {
		err := httpResponseError(rsp)
		return 0, fmt.Errorf("POST failure: %w", err)
	}

	var body struct {
		StartAt   int64 `json:"start_at"`
		Intervals []struct {
			EndAt            int64 `json:"end_at"`
			DevicesReporting int   `json:"devices_reporting"`
			WhDel            int64 `json:"wh_del"` // "units produced per interval" aka "Watt-Hours Delivered"
		}
	}

	if err := json.NewDecoder(rsp.Body).Decode(&body); err != nil {
		return 0, fmt.Errorf("failed to parse response body: %w", err)
	}

	if len(body.Intervals) == 0 {
		return 0, fmt.Errorf("no intervals  returned")
	}

	interval := body.Intervals[0]

	// sanity check the EndAt field...
	skew := time.Unix(interval.EndAt, 0).Sub(startAt.Add(15 * time.Minute))
	if skew < 0 {
		skew = -skew // absolute value
	}
	if skew > time.Minute {
		return 0, fmt.Errorf("untrustworthy interval: [%d, %d]", body.StartAt, interval.EndAt)
	}

	// multiple by 4 because X Watt*hours over 15 minutes is 4*X watts
	return interval.WhDel * 4, nil
}

// TODO(ianrose): implement paging
func (c *Client) FetchSystems(ctx context.Context, accessToken string, page int) ([]*System, error) {
	urlstr := fmt.Sprintf("https://api.enphaseenergy.com/api/v4/systems?size=100&page=%d", page)
	req, err := http.NewRequestWithContext(ctx, "GET", urlstr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to make new http request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("key", c.apiKey)

	rsp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET failure: %w", err)
	}

	defer drainAndClose(rsp.Body)

	if rsp.StatusCode >= 300 {
		err := httpResponseError(rsp)
		return nil, fmt.Errorf("POST failure: %w", err)
	}

	var body struct {
		Systems []*System
	}

	if err := json.NewDecoder(rsp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("failed to parse response body: %w", err)
	}

	return body.Systems, nil
}

func (c *Client) RefreshTokens(ctx context.Context, refreshToken string) (*OAuthTokenResponse, error) {
	qs := make(url.Values)
	qs.Set("grant_type", "refresh_token")
	qs.Set("refresh_token", refreshToken)

	urlstr := "https://api.enphaseenergy.com/oauth/token?" + qs.Encode()

	req, err := http.NewRequestWithContext(ctx, "POST", urlstr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to make new http request: %w", err)
	}
	req.SetBasicAuth(c.clientId, c.clientSecret)

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
