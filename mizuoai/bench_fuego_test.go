package mizuoai_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/brianvoe/gofakeit/v7"
	"github.com/go-fuego/fuego"
	"github.com/humbornjo/mizu"
	"github.com/humbornjo/mizu/mizuoai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func BenchmarkMizuOai_Small_Input(b *testing.B) {
	type Detail struct {
		Email  string `json:"email"`
		Role   string `json:"role"`
		Salary int    `json:"salary"`
		Pip    bool   `json:"pip"`
	}

	type InputBody struct {
		Name      string `json:"name"`
		Age       int    `json:"age"`
		Detail    Detail `json:"detail"`
		Biography string `json:"biography"`
	}
	type InputQuery struct {
		Source string `query:"source"`
		Branch bool   `query:"branch"`
	}

	var testCase = struct {
		name string
		data struct {
			Query InputQuery
			Body  InputBody
		}
	}{
		name: "JSON Body Request",
		data: struct {
			Query InputQuery
			Body  InputBody
		}{
			Query: InputQuery{Source: gofakeit.Word(), Branch: gofakeit.Bool()},
			Body: InputBody{
				Name:      gofakeit.Name(),
				Age:       gofakeit.Age(),
				Biography: gofakeit.LoremIpsumSentence(1024),
				Detail:    Detail{Email: gofakeit.Email(), Role: gofakeit.JobTitle(), Salary: gofakeit.Number(1e3, 1e5), Pip: gofakeit.Bool()},
			},
		},
	}

	consUrlWithQuery := func(base string, query InputQuery) string {
		return base + "?source=" + query.Source + "&branch=" + strconv.FormatBool(query.Branch)
	}

	b.Run("Mizu", func(b *testing.B) {
		type MizuInput struct {
			Body  InputBody  `mizu:"body"`
			Query InputQuery `mizu:"query"`
		}

		srv := mizu.NewServer("test")
		err := mizuoai.Initialize(srv, "test_title")
		require.NoError(b, err)

		mizuoai.Post(srv, "/users", func(tx mizuoai.Tx[InputBody], rx mizuoai.Rx[MizuInput]) {
			b.StartTimer()
			input, err := rx.MizuRead()
			b.StopTimer()

			require.NoError(b, err)
			require.Equal(b, testCase.data.Body, input.Body)
			require.Equal(b, testCase.data.Query, input.Query)
		})
		handlers := srv.Handler()

		b.ResetTimer()
		for range b.N {
			jsonb, err := json.Marshal(testCase.data.Body)
			assert.NoError(b, err)

			// Create a new request for each iteration to avoid body consumption issues
			req := httptest.NewRequest(
				http.MethodPost,
				consUrlWithQuery("/users", testCase.data.Query),
				bytes.NewBuffer(jsonb),
			)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handlers.ServeHTTP(w, req)
		}
	})

	b.Run("Fuego", func(b *testing.B) {
		srv := fuego.NewServer(fuego.WithoutLogger())
		fuego.Post(srv, "/users", func(ctx fuego.Context[InputBody, InputQuery]) (any, error) {
			b.StartTimer()
			body, berr := ctx.Body()
			params, perr := ctx.Params()
			b.StopTimer()

			require.NoError(b, berr)
			require.NoError(b, perr)

			require.Equal(b, testCase.data.Body, body)
			require.Equal(b, testCase.data.Query, params)

			return nil, nil
		})
		handlers := srv.Mux

		b.ResetTimer()
		for range b.N {
			jsonb, err := json.Marshal(testCase.data.Body)
			assert.NoError(b, err)

			req := httptest.NewRequest(
				http.MethodPost,
				consUrlWithQuery("/users", testCase.data.Query),
				bytes.NewBuffer(jsonb),
			)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handlers.ServeHTTP(w, req)
		}
	})
}

func BenchmarkMizuOai_Large_Input(b *testing.B) {
	type Project struct {
		Name         string   `json:"name"`
		Description  string   `json:"description"`
		StartDate    string   `json:"startDate"`
		EndDate      string   `json:"endDate"`
		Technologies []string `json:"technologies"`
		TeamSize     int      `json:"teamSize"`
	}

	type Address struct {
		Street  string `json:"street"`
		City    string `json:"city"`
		State   string `json:"state"`
		ZipCode string `json:"zipCode"`
		Country string `json:"country"`
	}

	type EmploymentHistory struct {
		Company          string   `json:"company"`
		Position         string   `json:"position"`
		StartDate        string   `json:"startDate"`
		EndDate          string   `json:"endDate"`
		Responsibilities []string `json:"responsibilities"`
	}

	type InputBody struct {
		Name              string              `json:"name"`
		Age               int                 `json:"age"`
		Email             string              `json:"email"`
		Phone             string              `json:"phone"`
		Addresses         []Address           `json:"addresses"`
		Biography         string              `json:"biography"`
		Skills            []string            `json:"skills"`
		EmploymentHistory []EmploymentHistory `json:"employmentHistory"`
		Projects          []Project           `json:"projects"`
		Metadata          map[string]any      `json:"metadata"`
	}

	type InputQuery struct {
		Source string `query:"source"`
		Branch bool   `query:"branch"`
		Search string `query:"search"`
		Page   int    `query:"page"`
		Limit  int    `query:"limit"`
	}

	// Generate large test data
	addresses := make([]Address, 5)
	for i := range addresses {
		addresses[i] = Address{
			Street:  gofakeit.Street(),
			City:    gofakeit.City(),
			State:   gofakeit.State(),
			ZipCode: gofakeit.Zip(),
			Country: gofakeit.Country(),
		}
	}

	skills := make([]string, 50)
	for i := range skills {
		skills[i] = gofakeit.JobDescriptor()
	}

	employmentHistory := make([]EmploymentHistory, 10)
	for i := range employmentHistory {
		responsibilities := make([]string, 20)
		for j := range responsibilities {
			responsibilities[j] = gofakeit.JobDescriptor()
		}
		employmentHistory[i] = EmploymentHistory{
			Company:          gofakeit.Company(),
			Position:         gofakeit.JobTitle(),
			StartDate:        gofakeit.Date().String(),
			EndDate:          gofakeit.Date().String(),
			Responsibilities: responsibilities,
		}
	}

	projects := make([]Project, 15)
	for i := range projects {
		technologies := make([]string, 10)
		for j := range technologies {
			technologies[j] = gofakeit.ProgrammingLanguage()
		}
		projects[i] = Project{
			Name:         gofakeit.AppName(),
			Description:  gofakeit.LoremIpsumParagraph(20, 10, 100, " "),
			StartDate:    gofakeit.Date().String(),
			EndDate:      gofakeit.Date().String(),
			Technologies: technologies,
			TeamSize:     gofakeit.Number(1, 50),
		}
	}

	metadata := map[string]any{
		"lastUpdated": gofakeit.Date().String(),
		"version":     gofakeit.AppVersion(),
		"tags":        gofakeit.Lexify("???????"),
		"score":       gofakeit.Float64Range(0, 10),
		"active":      gofakeit.Bool(),
		"preferences": map[string]any{
			"theme":         gofakeit.SafeColor(),
			"language":      gofakeit.Language(),
			"timezone":      gofakeit.TimeZone(),
			"notifications": gofakeit.Bool(),
		},
	}

	var testCase = struct {
		name string
		data struct {
			Query InputQuery
			Body  InputBody
		}
	}{
		name: "JSON Body Request",
		data: struct {
			Query InputQuery
			Body  InputBody
		}{
			Query: InputQuery{
				Source: gofakeit.Word(),
				Branch: gofakeit.Bool(),
				Search: gofakeit.LoremIpsumSentence(10),
				Page:   gofakeit.Number(1, 100),
				Limit:  gofakeit.Number(10, 100),
			},
			Body: InputBody{
				Name:              gofakeit.Name(),
				Age:               gofakeit.Age(),
				Email:             gofakeit.Email(),
				Phone:             gofakeit.Phone(),
				Addresses:         addresses,
				Biography:         gofakeit.LoremIpsumParagraph(100, 50, 200, " "),
				Skills:            skills,
				EmploymentHistory: employmentHistory,
				Projects:          projects,
				Metadata:          metadata,
			},
		},
	}

	consUrlWithQuery := func(base string, query InputQuery) string {
		params := url.Values{}
		params.Set("source", query.Source)
		params.Set("branch", strconv.FormatBool(query.Branch))
		params.Set("search", query.Search)
		params.Set("page", strconv.Itoa(query.Page))
		params.Set("limit", strconv.Itoa(query.Limit))
		return base + "?" + params.Encode()
	}

	b.Run("Mizu", func(b *testing.B) {
		type MizuInput struct {
			Body  InputBody  `mizu:"body"`
			Query InputQuery `mizu:"query"`
		}

		srv := mizu.NewServer("test")
		err := mizuoai.Initialize(srv, "test_title")
		require.NoError(b, err)

		mizuoai.Post(srv, "/users", func(tx mizuoai.Tx[InputBody], rx mizuoai.Rx[MizuInput]) {
			b.StartTimer()
			input, err := rx.MizuRead()
			b.StopTimer()

			require.NoError(b, err)
			require.Equal(b, testCase.data.Body.Name, input.Body.Name)
			require.Equal(b, testCase.data.Body.Email, input.Body.Email)
			require.Equal(b, testCase.data.Query.Source, input.Query.Source)
			require.Equal(b, testCase.data.Query.Branch, input.Query.Branch)
		})
		handlers := srv.Handler()

		b.ResetTimer()
		for range b.N {
			jsonb, err := json.Marshal(testCase.data.Body)
			assert.NoError(b, err)

			req := httptest.NewRequest(
				http.MethodPost,
				consUrlWithQuery("/users", testCase.data.Query),
				bytes.NewBuffer(jsonb),
			)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handlers.ServeHTTP(w, req)
		}
	})

	b.Run("Fuego", func(b *testing.B) {
		srv := fuego.NewServer(fuego.WithoutLogger())
		fuego.Post(srv, "/users", func(ctx fuego.Context[InputBody, InputQuery]) (any, error) {
			b.StartTimer()
			body, berr := ctx.Body()
			params, perr := ctx.Params()
			b.StopTimer()

			require.NoError(b, berr)
			require.NoError(b, perr)

			require.Equal(b, testCase.data.Body.Name, body.Name)
			require.Equal(b, testCase.data.Body.Email, body.Email)
			require.Equal(b, testCase.data.Query.Source, params.Source)
			require.Equal(b, testCase.data.Query.Branch, params.Branch)

			return nil, nil
		})
		handlers := srv.Mux

		b.ResetTimer()
		for range b.N {
			jsonb, err := json.Marshal(testCase.data.Body)
			assert.NoError(b, err)

			req := httptest.NewRequest(
				http.MethodPost,
				consUrlWithQuery("/users", testCase.data.Query),
				bytes.NewBuffer(jsonb),
			)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handlers.ServeHTTP(w, req)
		}
	})
}
