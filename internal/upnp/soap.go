package upnp

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"

	"go-upnp-player/internal/services"
)

// Arg is a SOAP action argument. Order matters: some renderers check it.
type Arg struct {
	Name  string
	Value string
}

// SOAPError is a UPnP error from a SOAP Fault (e.g. 701 Transition not available).
type SOAPError struct {
	Code        string
	Description string
	HTTPStatus  int
}

func (e *SOAPError) Error() string {
	return fmt.Sprintf("upnp error %s: %s (HTTP %d)", e.Code, e.Description, e.HTTPStatus)
}

// call performs the action and returns the output arguments.
func call(ctx context.Context, controlURL, service, action string, args []Arg) (map[string]string, error) {
	if controlURL == "" {
		return nil, fmt.Errorf("%s: service %s not available", action, service)
	}
	var body bytes.Buffer
	body.WriteString(`<?xml version="1.0" encoding="utf-8"?>`)
	body.WriteString(`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body>`)
	body.WriteString(`<u:` + action + ` xmlns:u="` + service + `">`)
	for _, a := range args {
		body.WriteString("<" + a.Name + ">" + services.EscapeXML(a.Value) + "</" + a.Name + ">")
	}
	body.WriteString(`</u:` + action + `></s:Body></s:Envelope>`)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, controlURL, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPACTION", `"`+service+"#"+action+`"`)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", action, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", action, err)
	}
	if resp.StatusCode != http.StatusOK {
		se := parseFault(data)
		se.HTTPStatus = resp.StatusCode
		return nil, fmt.Errorf("%s: %w", action, se)
	}
	return parseResponse(data, action+"Response")
}

// parseResponse extracts the child elements of <XxxResponse> as name → text.
func parseResponse(data []byte, element string) (map[string]string, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	out := make(map[string]string)
	depth := -1
	var cur string
	var text strings.Builder
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("soap response: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if depth < 0 {
				if t.Name.Local == element {
					depth = 0
				}
				continue
			}
			depth++
			if depth == 1 {
				cur = t.Name.Local
				text.Reset()
			}
		case xml.CharData:
			if depth == 1 {
				text.Write(t)
			}
		case xml.EndElement:
			if depth < 0 {
				continue
			}
			if depth == 1 {
				out[cur] = text.String()
			}
			if depth == 0 {
				return out, nil
			}
			depth--
		}
	}
}

func parseFault(data []byte) *SOAPError {
	var f struct {
		Code string `xml:"Body>Fault>detail>UPnPError>errorCode"`
		Desc string `xml:"Body>Fault>detail>UPnPError>errorDescription"`
	}
	_ = xml.Unmarshal(data, &f)
	if f.Code == "" {
		f.Code = "?"
	}
	return &SOAPError{Code: f.Code, Description: f.Desc}
}
