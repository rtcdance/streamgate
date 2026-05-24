package commonv1

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHealthCheckResponse_ServingStatus_Values(t *testing.T) {
	tests := []struct {
		name  string
		value HealthCheckResponse_ServingStatus
		want  int32
	}{
		{"UNKNOWN", HealthCheckResponse_UNKNOWN, 0},
		{"SERVING", HealthCheckResponse_SERVING, 1},
		{"NOT_SERVING", HealthCheckResponse_NOT_SERVING, 2},
		{"SERVICE_UNKNOWN", HealthCheckResponse_SERVICE_UNKNOWN, 3},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, int32(tc.value))
		})
	}
}

func TestMetadata_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		m := &Metadata{
			RequestId: "req-1",
			Timestamp: 1234567890,
			Version:   "1.0",
			Headers:   map[string]string{"x-custom": "val"},
		}
		assert.Equal(t, "req-1", m.GetRequestId())
		assert.Equal(t, int64(1234567890), m.GetTimestamp())
		assert.Equal(t, "1.0", m.GetVersion())
		assert.Equal(t, map[string]string{"x-custom": "val"}, m.GetHeaders())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var m *Metadata
		assert.Equal(t, "", m.GetRequestId())
		assert.Equal(t, int64(0), m.GetTimestamp())
		assert.Equal(t, "", m.GetVersion())
		assert.Nil(t, m.GetHeaders())
	})
}

func TestError_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		e := &Error{
			Code:      "ERR_001",
			Message:   "something failed",
			Details:   []string{"detail1", "detail2"},
			RequestId: "req-1",
		}
		assert.Equal(t, "ERR_001", e.GetCode())
		assert.Equal(t, "something failed", e.GetMessage())
		assert.Equal(t, []string{"detail1", "detail2"}, e.GetDetails())
		assert.Equal(t, "req-1", e.GetRequestId())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var e *Error
		assert.Equal(t, "", e.GetCode())
		assert.Equal(t, "", e.GetMessage())
		assert.Nil(t, e.GetDetails())
		assert.Equal(t, "", e.GetRequestId())
	})
}

func TestEmpty_ProtoMethods(t *testing.T) {
	e := &Empty{}
	assert.NotPanics(t, e.Reset)
	assert.NotPanics(t, e.ProtoMessage)
	_ = e.String()
}

func TestHealthCheckRequest_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		req := &HealthCheckRequest{Service: "auth"}
		assert.Equal(t, "auth", req.GetService())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var req *HealthCheckRequest
		assert.Equal(t, "", req.GetService())
	})
}

func TestHealthCheckResponse_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		resp := &HealthCheckResponse{Status: HealthCheckResponse_SERVING}
		assert.Equal(t, HealthCheckResponse_SERVING, resp.GetStatus())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var resp *HealthCheckResponse
		assert.Equal(t, HealthCheckResponse_UNKNOWN, resp.GetStatus())
	})
}

func TestPaginationRequest_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		req := &PaginationRequest{
			Page:      2,
			PageSize:  20,
			SortBy:    "created_at",
			SortOrder: "desc",
		}
		assert.Equal(t, int32(2), req.GetPage())
		assert.Equal(t, int32(20), req.GetPageSize())
		assert.Equal(t, "created_at", req.GetSortBy())
		assert.Equal(t, "desc", req.GetSortOrder())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var req *PaginationRequest
		assert.Equal(t, int32(0), req.GetPage())
		assert.Equal(t, int32(0), req.GetPageSize())
		assert.Equal(t, "", req.GetSortBy())
		assert.Equal(t, "", req.GetSortOrder())
	})
}

func TestPaginationResponse_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		resp := &PaginationResponse{
			Page:       1,
			PageSize:   10,
			Total:      100,
			TotalPages: 10,
		}
		assert.Equal(t, int32(1), resp.GetPage())
		assert.Equal(t, int32(10), resp.GetPageSize())
		assert.Equal(t, int32(100), resp.GetTotal())
		assert.Equal(t, int32(10), resp.GetTotalPages())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var resp *PaginationResponse
		assert.Equal(t, int32(0), resp.GetPage())
		assert.Equal(t, int32(0), resp.GetPageSize())
		assert.Equal(t, int32(0), resp.GetTotal())
		assert.Equal(t, int32(0), resp.GetTotalPages())
	})
}

func TestFilter_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		f := &Filter{Field: "status", Operator: "eq", Value: "active"}
		assert.Equal(t, "status", f.GetField())
		assert.Equal(t, "eq", f.GetOperator())
		assert.Equal(t, "active", f.GetValue())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var f *Filter
		assert.Equal(t, "", f.GetField())
		assert.Equal(t, "", f.GetOperator())
		assert.Equal(t, "", f.GetValue())
	})
}

func TestSearchRequest_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		filters := []*Filter{{Field: "status", Operator: "eq", Value: "active"}}
		pagination := &PaginationRequest{Page: 1, PageSize: 10}
		req := &SearchRequest{
			Query:      "test",
			Filters:    filters,
			Pagination: pagination,
		}
		assert.Equal(t, "test", req.GetQuery())
		assert.Equal(t, filters, req.GetFilters())
		assert.Equal(t, pagination, req.GetPagination())
	})

	t.Run("nil_nested", func(t *testing.T) {
		req := &SearchRequest{}
		assert.Nil(t, req.GetFilters())
		assert.Nil(t, req.GetPagination())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var req *SearchRequest
		assert.Equal(t, "", req.GetQuery())
		assert.Nil(t, req.GetFilters())
		assert.Nil(t, req.GetPagination())
	})
}

func TestSearchResponse_Getters(t *testing.T) {
	t.Run("with_values", func(t *testing.T) {
		pagination := &PaginationResponse{Total: 5}
		resp := &SearchResponse{
			Results:    []string{"r1", "r2"},
			Pagination: pagination,
		}
		assert.Equal(t, []string{"r1", "r2"}, resp.GetResults())
		assert.Equal(t, pagination, resp.GetPagination())
	})

	t.Run("nil_receiver", func(t *testing.T) {
		var resp *SearchResponse
		assert.Nil(t, resp.GetResults())
		assert.Nil(t, resp.GetPagination())
	})
}

func TestAllMessages_ProtoMethods(t *testing.T) {
	msgs := []interface {
		Reset()
		String() string
		ProtoMessage()
	}{
		&Metadata{},
		&Error{},
		&Empty{},
		&HealthCheckRequest{},
		&HealthCheckResponse{},
		&PaginationRequest{},
		&PaginationResponse{},
		&Filter{},
		&SearchRequest{},
		&SearchResponse{},
	}

	for _, msg := range msgs {
		assert.NotPanics(t, msg.Reset)
		assert.NotPanics(t, msg.ProtoMessage)
		_ = msg.String()
	}
}
