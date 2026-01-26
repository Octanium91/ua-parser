package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/ua-parser/uap-go/uaparser"
	"gopkg.in/yaml.v3"
)

func main() {
	yamlFile := "pkg/core/resources/regexes.yaml"
	jsonFile := "pkg/core/resources/regexes.json"

	data, err := os.ReadFile(yamlFile)
	if err != nil {
		log.Fatalf("failed to read yaml: %v", err)
	}

	def := uaparser.RegexDefinitions{}
	if err := yaml.Unmarshal(data, &def); err != nil {
		log.Fatalf("failed to unmarshal yaml: %v", err)
	}

	jsonData, err := json.Marshal(def)
	if err != nil {
		log.Fatalf("failed to marshal json: %v", err)
	}

	if err := os.WriteFile(jsonFile, jsonData, 0644); err != nil {
		log.Fatalf("failed to write json: %v", err)
	}

	fmt.Printf("Successfully converted %s to %s\n", yamlFile, jsonFile)
}
