package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const CEPAPIURL = "https://viacep.com.br/ws/"
const WeatherAPIURL = "https://api.weatherapi.com/v1/current.json?key=INSIRA_SUA_API_KEY&q="

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		BuscaDadosClima(w, r)
	})
	log.Printf("Servidor rodando em: http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func BuscaDadosClima(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cep := r.URL.Query().Get("cep")
	if cep == "" {
		http.Error(w, "empty zipcode", http.StatusBadRequest)
		return
	}

	if ok, _ := regexp.MatchString(`^\d{8}$`, cep); !ok {
		http.Error(w, "invalid zipcode", http.StatusUnprocessableEntity)
		return
	}

	log.Printf("Data received for CEP: %s", cep)

	cidade, err := BuscaDadosCep(cep)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			http.Error(w, "can not find zipcode", http.StatusNotFound)
			return
		}

		http.Error(w, "CEP API error", http.StatusInternalServerError)
		return
	}

	log.Printf("Cidade: %s", cidade)

	tempC, err := BuscaDadosCidade(cidade)
	if err != nil {
		http.Error(w, "Weather API error", http.StatusInternalServerError)
		return
	}

	tempCFloat, _ := strconv.ParseFloat(tempC, 64)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"temp_C": fmt.Sprintf("%.2f", tempCFloat),
		"temp_F": fmt.Sprintf("%.2f", (tempCFloat*9/5)+32),
		"temp_K": fmt.Sprintf("%.2f", tempCFloat+273.15),
	})
}

func BuscaDadosCep(cep string) (string, error) {
	httpReq, err := http.NewRequest(http.MethodGet, CEPAPIURL+cep+"/json/", nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", err
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var result map[string]interface{}
	err = json.Unmarshal(body, &result)
	if err != nil {
		return "", err
	}

	cidade, ok := result["localidade"].(string)
	if !ok {
		return "", fmt.Errorf("404")
	}

	return cidade, nil

}

func BuscaDadosCidade(cidade string) (string, error) {
	weatherURL, err := url.Parse(WeatherAPIURL)
	if err != nil {
		return "", err
	}
	query := weatherURL.Query()
	query.Set("q", cidade)
	weatherURL.RawQuery = query.Encode()

	httpReq, err := http.NewRequest(http.MethodGet, weatherURL.String(), nil)
	if err != nil {
		return "", err
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", err
	}

	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("Weather API returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var result map[string]interface{}
	err = json.Unmarshal(body, &result)
	if err != nil {
		return "", err
	}

	log.Printf("Temperature found: %v", result["current"].(map[string]interface{})["temp_c"])
	tempC, ok := result["current"].(map[string]interface{})["temp_c"].(float64)
	if !ok {
		return "", fmt.Errorf("temperature not found")
	}

	return fmt.Sprintf("%.2f", tempC), nil
}
