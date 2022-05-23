package httpserver

import (
	"net/http"
)

type ResponseWriterPeeker interface {
	http.ResponseWriter

	// GetStatus returns the status code written to the response, or 0 if none written yet.
	GetStatus() int

	// GetContentLength returns the number of bytes of response content written so far.
	GetContentLength() int
}

func NewResponseWriterPeeker(w http.ResponseWriter) ResponseWriterPeeker {
	return &responsePeeker{w: w}
}

type responsePeeker struct {
	w      http.ResponseWriter
	status int
	len    int
}

func (p *responsePeeker) GetStatus() int {
	if p.status == 0 {
		return http.StatusOK
	}
	return p.status
}

func (p *responsePeeker) GetContentLength() int {
	return p.len
}

func (p *responsePeeker) Header() http.Header {
	return p.w.Header()
}

func (p *responsePeeker) Write(b []byte) (int, error) {
	if p.status == 0 {
		p.status = http.StatusOK
	}

	n, err := p.w.Write(b)
	p.len += n
	return n, err
}

func (p *responsePeeker) WriteHeader(statusCode int) {
	if p.status == 0 {
		p.status = statusCode
	}
	p.w.WriteHeader(statusCode)
}
