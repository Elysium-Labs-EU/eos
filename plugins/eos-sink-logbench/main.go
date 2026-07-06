package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

type record struct {
	Stream string `json:"stream"`
	Msg    string `json:"msg"`
}

type ingestBody struct {
	Content string `json:"content"`
	Level   string `json:"level"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	if err := run(ctx, os.Stdin); err != nil {
		stop()
		fmt.Fprintf(os.Stderr, "eos-sink-logbench: %v\n", err)
		os.Exit(1)
	}
	stop()
}

func run(ctx context.Context, in io.Reader) error {
	var options map[string]any
	if err := json.Unmarshal([]byte(os.Getenv("EOS_SINK_OPTIONS")), &options); err != nil {
		return fmt.Errorf("parse options: %w", err)
	}

	projectID, _ := options["project_id"].(string)
	if projectID == "" {
		return fmt.Errorf("missing required option project_id")
	}

	endpoint, _ := options["endpoint"].(string)
	if endpoint == "" {
		endpoint = "http://localhost:1447"
	}
	endpoint = strings.TrimRight(endpoint, "/")

	url := fmt.Sprintf("%s/api/projects/%s/logs/ingest", endpoint, projectID)
	client := &http.Client{Timeout: 5 * time.Second}

	fmt.Println("READY")
	fmt.Fprintf(os.Stderr, "eos-sink-logbench: ready — endpoint=%s project=%s\n", endpoint, projectID)

	firstSuccess := false
	sc := bufio.NewScanner(in)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}

		var rec record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			fmt.Fprintf(os.Stderr, "eos-sink-logbench: parse record: %v\n", err)
			continue
		}

		level := "LOG"
		if rec.Stream == "stderr" {
			level = "ERROR"
		}

		body, _ := json.Marshal(ingestBody{Content: rec.Msg, Level: level})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			fmt.Fprintf(os.Stderr, "eos-sink-logbench: build request: %v\n", err)
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "eos-sink-logbench: post: %v\n", err)
			continue
		}
		if resp != nil {
			if closeErr := resp.Body.Close(); closeErr != nil {
				fmt.Fprintf(os.Stderr, "eos-sink-logbench: close body: %v\n", closeErr)
			}
			if !firstSuccess {
				fmt.Fprintf(os.Stderr, "eos-sink-logbench: first record delivered (status %d)\n", resp.StatusCode)
				firstSuccess = true
			}
		}
	}
	return nil
}
