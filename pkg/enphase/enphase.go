package enphase

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
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

func CompleteAuthorization(ctx context.Context, authCode, redirectUrl, clientId, clientSecret string) (*OAuthTokenResponse, error) {
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
	req.SetBasicAuth(string(clientId), string(clientSecret))

	rsp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST failure: %w", err)
	}

	defer func() {
		ioutil.ReadAll(rsp.Body)
		rsp.Body.Close()
	}()

	if rsp.StatusCode >= 300 {
		return nil, fmt.Errorf("POST failure: %s", rsp.Status)
	}

	var body OAuthTokenResponse
	if err := json.NewDecoder(rsp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("failed to parse response body: %w", err)
	}

	return &body, nil
}

func FetchConsumption(ctx context.Context, systemId int64, accessToken, apiKey string, startAt time.Time) (int64, error) {
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
	req.Header.Set("key", apiKey)

	rsp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("GET failure: %w", err)
	}

	defer func() {
		ioutil.ReadAll(rsp.Body)
		rsp.Body.Close()
	}()

	if rsp.StatusCode >= 300 {
		body, _ := ioutil.ReadAll(io.LimitReader(rsp.Body, 512))
		return 0, fmt.Errorf("GET failure: (%s) %s", rsp.Status, string(body))
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

	var watts int64
	for _, interval := range body.Intervals {
		// multiple by 4 because X Watt*hours over 15 minutes is 4*X watts
		watts += interval.EnWh * 4
	}

	return watts, nil
}

func FetchProduction(ctx context.Context, systemId int64, accessToken, apiKey string, startAt time.Time) (int64, error) {
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
	req.Header.Set("key", apiKey)

	rsp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("GET failure: %w", err)
	}

	defer func() {
		ioutil.ReadAll(rsp.Body)
		rsp.Body.Close()
	}()

	if rsp.StatusCode >= 300 {
		body, _ := ioutil.ReadAll(io.LimitReader(rsp.Body, 512))
		return 0, fmt.Errorf("GET failure: (%s) %s", rsp.Status, string(body))
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

	var watts int64
	for _, interval := range body.Intervals {
		// multiple by 4 because X Watt*hours over 15 minutes is 4*X watts
		watts += interval.WhDel * 4
	}

	return watts, nil
}

// TODO(ianrose): implement paging
func FetchSystems(ctx context.Context, accessToken, apiKey string) ([]*System, error) {
	const urlstr = "https://api.enphaseenergy.com/api/v4/systems?size=100"

	req, err := http.NewRequestWithContext(ctx, "GET", urlstr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to make new http request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("key", apiKey)

	rsp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET failure: %w", err)
	}

	defer func() {
		ioutil.ReadAll(rsp.Body)
		rsp.Body.Close()
	}()

	if rsp.StatusCode >= 300 {
		return nil, fmt.Errorf("GET failure: %s", rsp.Status)
	}

	var body struct {
		Systems []*System
	}

	if err := json.NewDecoder(rsp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("failed to parse response body: %w", err)
	}

	return body.Systems, nil
}
