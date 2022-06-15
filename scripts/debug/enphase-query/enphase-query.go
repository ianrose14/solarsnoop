package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/ianrose14/solarsnoop/internal"
	"github.com/ianrose14/solarsnoop/pkg/enphase"
)

func main() {
	accessToken := flag.String("token", "", "Enlighten oauth2 access token (required)")
	systemId := flag.Int64("systemId", 0, "System to query (optional)")
	flag.Parse()

	if *accessToken == "" {
		log.Fatalf("-token is required")
	}

	ctx := context.Background()

	secrets, err := internal.ParseSecrets("config/secrets.yaml")
	if err != nil {
		log.Fatalf("failed to read secrets: %+v", err)
	}

	enphaseClient := enphase.NewClient(secrets.Enphase.ApiKey, secrets.Enphase.ClientID, secrets.Enphase.ClientSecret)

	if *systemId == 0 {
		for page := 1; ; page++ {
			systems, err := enphaseClient.FetchSystems(ctx, *accessToken, page)
			if err != nil {
				log.Fatalf("failed to query systems: %+v", err)
			}

			log.Printf("page %d: got %d systems", page, len(systems))
			for i, sys := range systems {
				log.Printf("%d: %+v", 100*page+i, sys)
			}

			if len(systems) < 100 {
				break
			}
		}
		return
	}

	fetchOneSystem(ctx, *accessToken, secrets.Enphase.ApiKey, *systemId)

	watts, err := enphaseClient.FetchConsumption(ctx, *systemId, *accessToken, time.Now().Add(-7*24*time.Hour))
	if err != nil {
		log.Printf("failed to query consumption: %+v", err)
	} else {
		log.Printf("consumption: %d watts", watts)
	}

	watts, err = enphaseClient.FetchProduction(ctx, *systemId, *accessToken, time.Now().Add(-7*24*time.Hour))
	if err != nil {
		log.Printf("failed to query production: %+v", err)
	} else {
		log.Printf("production: %d watts", watts)
	}
}

func fetchOneSystem(ctx context.Context, accessToken, apiKey string, systemId int64) {
	urlstr := fmt.Sprintf("https://api.enphaseenergy.com/api/v4/systems/%d", systemId)

	req, err := http.NewRequestWithContext(ctx, "GET", urlstr, nil)
	if err != nil {
		log.Printf("failed to make new http request: %w", err)
		return
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("key", apiKey)

	rsp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("GET failure: %w", err)
		return
	}

	if rsp.StatusCode >= 300 {
		body, _ := ioutil.ReadAll(io.LimitReader(rsp.Body, 512))
		log.Printf("GET failure: (%s) %s", rsp.Status, string(body))
		return
	}

	body, err := ioutil.ReadAll(rsp.Body)
	if err != nil {
		log.Printf("failed to read: %+v", err)
		return
	}

	log.Printf("body: %s", string(body))
}
