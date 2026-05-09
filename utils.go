package main

import (
	"encoding/json"
	"time"
)

func nowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func prettyJSON(v interface{}) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}
