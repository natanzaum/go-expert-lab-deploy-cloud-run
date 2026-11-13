package main

import (
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

// setupMocks redireciona as chamadas para a ViaCEP e a WeatherAPI
// para servidores de teste, sem alterar as URLs da aplicação.
func setupMocks(t *testing.T, viacepHandler, weatherHandler http.HandlerFunc) {
	t.Helper()

	mux := http.NewServeMux()
	if viacepHandler != nil {
		mux.HandleFunc("/ws/", viacepHandler)
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

// doReq executa o handler da aplicação.
func doReq(url string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()
	BuscaDadosClima(w, req)
	return w
}

func TestInvalidCEP(t *testing.T) {
	w := doReq("/?cep=123")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("esperado 422, veio %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "invalid zipcode") {
		t.Fatalf("resposta errada: %s", w.Body.String())
	}
}

func TestEmptyCEP(t *testing.T) {
	w := doReq("/")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, veio %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "empty zipcode") {
		t.Fatalf("resposta errada: %s", w.Body.String())
	}
}

func TestCEPNotFound(t *testing.T) {
	setupMocks(t,
		func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `{"erro": true}`)
		},
		nil,
	)

	w := doReq("/?cep=00000000")
	if w.Code != http.StatusNotFound {
		t.Fatalf("esperado 404, veio %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "can not find zipcode") {
		t.Fatalf("resposta errada: %s", w.Body.String())
	}
}

func TestSuccess(t *testing.T) {
	// mock da ViaCEP
	setupMocks(t,
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/ws/01001000/json/" {
				t.Errorf("path da ViaCEP = %q, esperado %q", r.URL.Path, "/ws/01001000/json/")
			}
			io.WriteString(w, `{"localidade":"São Paulo","uf":"SP"}`)
		},
		// mock da WeatherAPI
		func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Query().Get("q"); got != "São Paulo" {
				t.Errorf("query q = %q, esperado %q", got, "São Paulo")
			}
			io.WriteString(w, `{"current":{"temp_c":25.0}}`)
		},
	)

	w := doReq("/?cep=01001000")
	if w.Code != http.StatusOK {
		t.Fatalf("esperado 200, veio %d. body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"temp_C":"25.00"`) {
		t.Fatalf("resposta errada: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"temp_F":"77.00"`) {
		t.Fatalf("resposta errada: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"temp_K":"298.15"`) {
		t.Fatalf("resposta errada: %s", w.Body.String())
	}
}

func TestWeatherAPIError(t *testing.T) {
	setupMocks(t,
		func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `{"localidade":"São Paulo"}`)
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, `{"error":{"message":"erro"}}`)
		},
	)

	w := doReq("/?cep=01001000")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("esperado 500, veio %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Weather API error") {
		t.Fatalf("resposta errada: %s", w.Body.String())
	}
}
