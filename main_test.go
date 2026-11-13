package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// roundTripFunc adapta uma função para a interface http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

// setupAPIs sobe um servidor de teste e redireciona todas as chamadas
// HTTP feitas pelo código para ele, sem alterar as URLs de produção.
func setupAPIs(t *testing.T, cepHandler, weatherHandler http.HandlerFunc) {
	t.Helper()

	mux := http.NewServeMux()
	if cepHandler != nil {
		mux.HandleFunc("/ws/", cepHandler)
	}
	if weatherHandler != nil {
		mux.HandleFunc("/v1/current.json", weatherHandler)
	}

	srv := httptest.NewServer(mux)

	target, err := url.Parse(srv.URL)
	if err != nil {
		srv.Close()
		t.Fatalf("erro ao interpretar URL do servidor de teste: %v", err)
	}

	originalClient := http.DefaultClient
	http.DefaultClient = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			clone := r.Clone(r.Context())
			clone.URL.Scheme = target.Scheme
			clone.URL.Host = target.Host
			clone.Host = target.Host
			return http.DefaultTransport.RoundTrip(clone)
		}),
	}

	t.Cleanup(func() {
		http.DefaultClient = originalClient
		srv.Close()
	})
}

// doRequest executa o handler de clima e devolve o response recorder.
func doRequest(t *testing.T, method, target string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, target, nil)
	rec := httptest.NewRecorder()
	BuscaDadosClima(rec, req)
	return rec
}

func TestBuscaDadosClima_MethodNotAllowed(t *testing.T) {
	methods := []string{
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodHead,
		http.MethodOptions,
	}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			rec := doRequest(t, method, "/?cep=12345678")

			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("status = %d, esperado %d", rec.Code, http.StatusMethodNotAllowed)
			}
			if got := strings.TrimSpace(rec.Body.String()); got != "Method not allowed" {
				t.Errorf("corpo = %q, esperado %q", got, "Method not allowed")
			}
		})
	}
}

func TestBuscaDadosClima_MissingCEP(t *testing.T) {
	rec := doRequest(t, http.MethodGet, "/")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, esperado %d", rec.Code, http.StatusBadRequest)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "empty zipcode" {
		t.Errorf("corpo = %q, esperado %q", got, "empty zipcode")
	}
}

func TestBuscaDadosClima_InvalidCEP(t *testing.T) {
	ceps := []string{
		"1234567",
		"123456789",
		"abcdefgh",
		"1234567a",
		"1234 567",
		"",
	}

	for _, cep := range ceps {
		t.Run(cep, func(t *testing.T) {
			target := "/?cep=" + url.QueryEscape(cep)
			rec := doRequest(t, http.MethodGet, target)

			statusEsperado := http.StatusUnprocessableEntity
			corpoEsperado := "invalid zipcode"

			if cep == "" {
				statusEsperado = http.StatusBadRequest
				corpoEsperado = "empty zipcode"
			}

			if rec.Code != statusEsperado {
				t.Errorf("status = %d, esperado %d", rec.Code, statusEsperado)
			}
			if got := strings.TrimSpace(rec.Body.String()); got != corpoEsperado {
				t.Errorf("corpo = %q, esperado %q", got, corpoEsperado)
			}
		})
	}
}

func TestBuscaDadosClima_Success(t *testing.T) {
	setupAPIs(t,
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/ws/12345678/json/" {
				t.Errorf("path da ViaCEP = %q, esperado %q", r.URL.Path, "/ws/12345678/json/")
			}
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"cep":"12345678","localidade":"São Paulo"}`)
		},
		func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Query().Get("q"); got != "São Paulo" {
				t.Errorf("query q = %q, esperado %q", got, "São Paulo")
			}
			io.WriteString(w, `{"current":{"temp_c":25.5}}`)
		},
	)

	rec := doRequest(t, http.MethodGet, "/?cep=12345678")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado %d (corpo: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body struct {
		TempC string `json:"temp_C"`
		TempF string `json:"temp_F"`
		TempK string `json:"temp_K"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("erro ao decodificar resposta: %v (corpo: %s)", err, rec.Body.String())
	}

	esperado := struct {
		TempC string
		TempF string
		TempK string
	}{
		TempC: "25.50",
		TempF: "77.90",
		TempK: "298.65",
	}

	if body.TempC != esperado.TempC {
		t.Errorf("temp_C = %q, esperado %q", body.TempC, esperado.TempC)
	}
	if body.TempF != esperado.TempF {
		t.Errorf("temp_F = %q, esperado %q", body.TempF, esperado.TempF)
	}
	if body.TempK != esperado.TempK {
		t.Errorf("temp_K = %q, esperado %q", body.TempK, esperado.TempK)
	}
}

func TestBuscaDadosClima_CEPNotFound(t *testing.T) {
	setupAPIs(t,
		func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `{}`)
		},
		func(w http.ResponseWriter, r *http.Request) {
			t.Error("a Weather API não deveria ser chamada quando o CEP não é encontrado")
		},
	)

	rec := doRequest(t, http.MethodGet, "/?cep=12345678")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, esperado %d", rec.Code, http.StatusNotFound)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "can not find zipcode" {
		t.Errorf("corpo = %q, esperado %q", got, "can not find zipcode")
	}
}

func TestBuscaDadosClima_CEPAPIError(t *testing.T) {
	setupAPIs(t,
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, `Internal Server Error`)
		},
		func(w http.ResponseWriter, r *http.Request) {
			t.Error("a Weather API não deveria ser chamada quando a ViaCEP falha")
		},
	)

	rec := doRequest(t, http.MethodGet, "/?cep=12345678")

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, esperado %d", rec.Code, http.StatusInternalServerError)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "CEP API error" {
		t.Errorf("corpo = %q, esperado %q", got, "CEP API error")
	}
}

func TestBuscaDadosClima_WeatherAPIError(t *testing.T) {
	setupAPIs(t,
		func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `{"localidade":"São Paulo"}`)
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, `{"error":{"message":"erro"}}`)
		},
	)

	rec := doRequest(t, http.MethodGet, "/?cep=12345678")

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, esperado %d", rec.Code, http.StatusInternalServerError)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "Weather API error" {
		t.Errorf("corpo = %q, esperado %q", got, "Weather API error")
	}
}

func TestBuscaDadosCep_Success(t *testing.T) {
	setupAPIs(t,
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/ws/12345678/json/" {
				t.Errorf("path = %q, esperado %q", r.URL.Path, "/ws/12345678/json/")
			}
			io.WriteString(w, `{"cep":"12345678","localidade":"São Paulo"}`)
		},
		nil,
	)

	cidade, err := BuscaDadosCep("12345678")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if cidade != "São Paulo" {
		t.Errorf("cidade = %q, esperado %q", cidade, "São Paulo")
	}
}

func TestBuscaDadosCep_NotFound(t *testing.T) {
	setupAPIs(t,
		func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `{}`)
		},
		nil,
	)

	_, err := BuscaDadosCep("12345678")
	if err == nil {
		t.Fatal("esperava um erro para CEP inexistente, mas não recebi nenhum")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("erro = %q, esperado conter %q", err.Error(), "404")
	}
}

func TestBuscaDadosCep_InvalidJSON(t *testing.T) {
	setupAPIs(t,
		func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `not-json`)
		},
		nil,
	)

	_, err := BuscaDadosCep("12345678")
	if err == nil {
		t.Fatal("esperava erro de JSON inválido, mas não recebi nenhum")
	}
}

func TestBuscaDadosCidade_Success(t *testing.T) {
	setupAPIs(t,
		nil,
		func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Query().Get("q"); got != "São Paulo" {
				t.Errorf("query q = %q, esperado %q", got, "São Paulo")
			}
			io.WriteString(w, `{"current":{"temp_c":25.5}}`)
		},
	)

	temp, err := BuscaDadosCidade("São Paulo")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if temp != "25.50" {
		t.Errorf("temp = %q, esperado %q", temp, "25.50")
	}
}

func TestBuscaDadosCidade_HTTPError(t *testing.T) {
	setupAPIs(t,
		nil,
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		},
	)

	_, err := BuscaDadosCidade("São Paulo")
	if err == nil {
		t.Fatal("esperava erro para status HTTP 502, mas não recebi nenhum")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("erro = %q, esperado conter %q", err.Error(), "502")
	}
}

func TestBuscaDadosCidade_MissingTemperature(t *testing.T) {
	setupAPIs(t,
		nil,
		func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `{"current":{}}`)
		},
	)

	_, err := BuscaDadosCidade("São Paulo")
	if err == nil {
		t.Fatal("esperava erro para temperatura ausente, mas não recebi nenhum")
	}
	if !strings.Contains(err.Error(), "temperature not found") {
		t.Errorf("erro = %q, esperado conter %q", err.Error(), "temperature not found")
	}
}

func TestBuscaDadosCidade_InvalidJSON(t *testing.T) {
	setupAPIs(t,
		nil,
		func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `not-json`)
		},
	)

	_, err := BuscaDadosCidade("São Paulo")
	if err == nil {
		t.Fatal("esperava erro de JSON inválido, mas não recebi nenhum")
	}
}
