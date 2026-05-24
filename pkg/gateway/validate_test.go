package gateway

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
	ut "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testValidateStruct struct {
	Name    string `json:"name" binding:"required"`
	Email   string `json:"email" binding:"required,email"`
	ChainID int64  `json:"chain_id" binding:"chain_id"`
	Address string `json:"address" binding:"omitempty,address"`
}

func TestBindAndValidate_ValidBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"name":"test","email":"test@example.com","chain_id":1}`
	c.Request = httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")

	var req testValidateStruct
	errs := BindAndValidate(c, &req)
	assert.Nil(t, errs)
	assert.Equal(t, "test", req.Name)
	assert.Equal(t, "test@example.com", req.Email)
	assert.Equal(t, int64(1), req.ChainID)
}

func TestBindAndValidate_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/test", bytes.NewBufferString("not json"))
	c.Request.Header.Set("Content-Type", "application/json")

	var req testValidateStruct
	errs := BindAndValidate(c, &req)
	assert.NotNil(t, errs)
	assert.Contains(t, errs["_error"], "invalid JSON body")
}

func TestBindAndValidate_MissingRequired(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"email":"test@example.com"}`
	c.Request = httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")

	var req testValidateStruct
	errs := BindAndValidate(c, &req)
	assert.NotNil(t, errs)
	_, hasName := errs["name"]
	assert.True(t, hasName)
}

func TestBindAndValidate_InvalidEmail(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"name":"test","email":"not-an-email"}`
	c.Request = httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")

	var req testValidateStruct
	errs := BindAndValidate(c, &req)
	assert.NotNil(t, errs)
	_, hasEmail := errs["email"]
	assert.True(t, hasEmail)
}

func TestBindAndValidate_InvalidAddress(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"name":"test","email":"test@example.com","address":"0x123"}`
	c.Request = httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")

	var req testValidateStruct
	errs := BindAndValidate(c, &req)
	assert.NotNil(t, errs)
}

func TestBindAndValidate_InvalidChainID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"name":"test","email":"test@example.com","chain_id":0}`
	c.Request = httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")

	var req testValidateStruct
	errs := BindAndValidate(c, &req)
	assert.NotNil(t, errs)
}

func TestBindAndValidate_EmptyBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/test", http.NoBody)
	c.Request.Header.Set("Content-Type", "application/json")

	var req testValidateStruct
	errs := BindAndValidate(c, &req)
	assert.NotNil(t, errs)
}

func TestBindAndValidate_ValidWithAddress(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"name":"test","email":"test@example.com","chain_id":1,"address":"0x1234567890123456789012345678901234567890"}`
	c.Request = httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")

	var req testValidateStruct
	errs := BindAndValidate(c, &req)
	require.Nil(t, errs)
	assert.Equal(t, "0x1234567890123456789012345678901234567890", req.Address)
}

func TestSnakeField(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Name", "name"},
		{"EmailAddress", "email_address"},
		{"simple", "simple"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, snakeField(tt.input))
		})
	}
}

func TestValidationMessage(t *testing.T) {
	tests := []struct {
		tag   string
		param string
		want  string
	}{
		{"required", "", "required"},
		{"min", "5", "minimum 5"},
		{"max", "100", "maximum 100"},
		{"gte", "1", "must be >= 1"},
		{"lte", "10", "must be <= 10"},
		{"oneof", "a b c", "must be one of: a b c"},
		{"email", "", "email"},
	}

	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			fe := &fieldErrorStub{tag: tt.tag, param: tt.param}
			assert.Equal(t, tt.want, validationMessage(fe))
		})
	}
}

type fieldErrorStub struct {
	tag   string
	param string
}

func (e *fieldErrorStub) Tag() string             { return e.tag }
func (e *fieldErrorStub) ActualTag() string       { return e.tag }
func (e *fieldErrorStub) Namespace() string       { return "" }
func (e *fieldErrorStub) StructNamespace() string  { return "" }
func (e *fieldErrorStub) Field() string            { return "Test" }
func (e *fieldErrorStub) StructField() string      { return "" }
func (e *fieldErrorStub) Value() interface{}       { return "" }
func (e *fieldErrorStub) Param() string            { return e.param }
func (e *fieldErrorStub) Kind() reflect.Kind       { return reflect.String }
func (e *fieldErrorStub) Type() reflect.Type       { return reflect.TypeOf("") }
func (e *fieldErrorStub) Translate(_ ut.Translator) string { return "" }
func (e *fieldErrorStub) Error() string            { return e.tag }

var _ validator.FieldError = (*fieldErrorStub)(nil)
