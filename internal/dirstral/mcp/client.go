package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"dir2mcp/internal/config"
	"dir2mcp/internal/protocol"
	"dir2mcp/internal/x402"
)

const protocolVersion = config.DefaultProtocolVersion

type Client struct {
	endpoint   string
	transport  string
	verbose    bool
	authToken  string
	mu         sync.Mutex
	sessionID  string
	httpClient *http.Client
	stdio      *stdioClient
	nextID     int
}

type stdioClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	mu     sync.Mutex
	callMu sync.Mutex
}

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type ContentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type ToolCallResult struct {
	Content           []ContentItem     `json:"content"`
	StructuredContent map[string]any    `json:"structuredContent"`
	IsError           bool              `json:"isError"`
	Raw               map[string]any    `json:"-"`
	Elapsed           time.Duration     `json:"-"`
	HTTPStatus        int               `json:"-"`
	Headers           map[string]string `json:"-"`
}

type jsonRPCRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      *int           `json:"id,omitempty"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Result  any    `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code                   int
	Message                string
	HTTPStatus             int
	Headers                map[string]string
	PaymentRequiredHeader  *x402.X402Payload
	PaymentResponseHeader  string
	PaymentRequiredPresent bool
}

func (e *jsonRPCError) Error() string {
	if e == nil {
		return ""
	}
	if e.HTTPStatus <= 0 {
		return fmt.Sprintf("json-rpc error %d: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("json-rpc error %d: %s (http %d)", e.Code, e.Message, e.HTTPStatus)
}

func (e *jsonRPCError) isPaymentRequired() bool {
	if e == nil {
		return false
	}
	if e.HTTPStatus != http.StatusPaymentRequired {
		return false
	}
	return e.PaymentRequiredPresent || e.PaymentRequiredHeader != nil || strings.TrimSpace(e.PaymentResponseHeader) != ""
}

func New(endpoint string, verbose bool) *Client {
	return NewWithTransport(endpoint, "streamable-http", verbose)
}

func NewWithTransport(endpoint, transport string, verbose bool) *Client {
	endpoint = strings.TrimSpace(endpoint)
	transport = strings.TrimSpace(strings.ToLower(transport))
	if transport == "" {
		transport = "streamable-http"
	}
	return &Client{
		endpoint:  endpoint,
		transport: transport,
		verbose:   verbose,
		authToken: strings.TrimSpace(os.Getenv("DIR2MCP_AUTH_TOKEN")),
		httpClient: &http.Client{
			Timeout: 45 * time.Second,
		},
		nextID: 1,
	}
}

func (c *Client) Close() error {
	if c.stdio == nil {
		return nil
	}
	if c.stdio.stdin != nil {
		_ = c.stdio.stdin.Close()
	}
	err := c.stdio.cmd.Wait()
	c.stdio = nil
	if err == nil {
		return nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 0 {
		return nil
	}
	return err
}

func (c *Client) Initialize(ctx context.Context) error {
	if c.transport == "stdio" {
		if err := c.startStdio(ctx); err != nil {
			return err
		}
	}
	params := map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"clientInfo":      map[string]any{"name": "dirstral", "version": "0.1.0"},
	}
	_, status, headers, err := c.call(ctx, protocol.RPCMethodInitialize, params, true)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("initialize failed with http status %d", status)
	}
	if c.transport == "streamable-http" {
		sessionID := headers.Get(protocol.MCPSessionHeader)
		if sessionID == "" {
			return fmt.Errorf("initialize response missing %s", protocol.MCPSessionHeader)
		}
		c.mu.Lock()
		c.sessionID = sessionID
		c.mu.Unlock()
	} else {
		c.mu.Lock()
		c.sessionID = "stdio"
		c.mu.Unlock()
	}

	_, notifyStatus, _, notifyErr := c.call(ctx, protocol.RPCMethodNotificationsInitialized, map[string]any{}, false)
	if notifyErr != nil {
		return fmt.Errorf("notifications/initialized failed: %w", notifyErr)
	}
	if notifyStatus != http.StatusAccepted && (notifyStatus < 200 || notifyStatus >= 300) {
		return fmt.Errorf("notifications/initialized returned status %d", notifyStatus)
	}
	return nil
}

func (c *Client) SessionID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessionID
}

func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	body, _, _, err := c.call(ctx, protocol.RPCMethodToolsList, map[string]any{}, true)
	if err != nil {
		return nil, err
	}
	result, ok := body["result"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid tools/list result")
	}
	items, ok := result["tools"].([]any)
	if !ok {
		return nil, fmt.Errorf("invalid tools/list payload")
	}

	tools := make([]Tool, 0, len(items))
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		tools = append(tools, Tool{
			Name:        asString(m["name"]),
			Description: asString(m["description"]),
			InputSchema: asMap(m["inputSchema"]),
		})
	}
	return tools, nil
}

func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error) {
	params := map[string]any{
		"name":      name,
		"arguments": args,
	}
	start := time.Now()
	body, status, headers, err := c.call(ctx, protocol.RPCMethodToolsCall, params, true)
	if err != nil && c.transport == "streamable-http" && isSessionNotFoundError(err) {
		if c.verbose {
			fmt.Println("[mcp] SESSION_NOT_FOUND received; recovering session and retrying tools/call once")
		}
		if recoverErr := c.recoverStreamableHTTPSession(ctx); recoverErr != nil {
			if c.verbose {
				fmt.Printf("[mcp] session recovery failed: %v\n", recoverErr)
			}
			return nil, fmt.Errorf("session recovery failed: %w", recoverErr)
		}
		if c.verbose {
			fmt.Println("[mcp] session recovery succeeded; retrying tools/call")
		}
		body, status, headers, err = c.call(ctx, protocol.RPCMethodToolsCall, params, true)
		if err != nil && c.verbose {
			fmt.Printf("[mcp] tools/call retry failed: %v\n", err)
		}
	}
	if err != nil {
		return nil, err
	}
	resultMap, ok := body["result"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid tools/call result")
	}
	content := []ContentItem{}
	if items, ok := resultMap["content"].([]any); ok {
		for _, it := range items {
			m, ok := it.(map[string]any)
			if !ok {
				continue
			}
			content = append(content, ContentItem{Type: asString(m["type"]), Text: asString(m["text"])})
		}
	}
	structured := map[string]any{}
	if sc, ok := resultMap["structuredContent"].(map[string]any); ok {
		structured = sc
	}
	out := &ToolCallResult{
		Content:           content,
		StructuredContent: structured,
		IsError:           asBool(resultMap["isError"]),
		Raw:               body,
		Elapsed:           time.Since(start),
		HTTPStatus:        status,
		Headers:           flattenHeaders(headers),
	}
	return out, nil
}

func (c *Client) recoverStreamableHTTPSession(ctx context.Context) error {
	if c.verbose {
		fmt.Println("[mcp] recovering streamable-http MCP session")
	}
	c.mu.Lock()
	previousSessionID := c.sessionID
	c.sessionID = ""
	c.mu.Unlock()
	if err := c.Initialize(ctx); err != nil {
		c.mu.Lock()
		c.sessionID = previousSessionID
		c.mu.Unlock()
		return err
	}
	return nil
}

func isSessionNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	code := CanonicalCodeFromError(err)
	return code == protocol.ErrorCodeSessionNotFound
}

func (c *Client) call(ctx context.Context, method string, params map[string]any, withID bool) (map[string]any, int, http.Header, error) {
	if c.transport == "stdio" {
		return c.callStdio(ctx, method, params, withID)
	}
	if c.transport != "streamable-http" {
		return nil, 0, nil, fmt.Errorf("unsupported transport %q", c.transport)
	}

	var id *int
	if withID {
		c.mu.Lock()
		n := c.nextID
		c.nextID++
		c.mu.Unlock()
		id = &n
	}
	message := jsonRPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	payload, err := json.Marshal(message)
	if err != nil {
		return nil, 0, nil, err
	}

	if c.verbose {
		fmt.Printf("\n[mcp] -> %s\n", method)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(protocol.MCPProtocolVersionHeader, protocolVersion)
	req.Header.Set("Accept", "application/json, text/event-stream")
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}
	c.mu.Lock()
	sessionID := c.sessionID
	c.mu.Unlock()
	if sessionID != "" {
		req.Header.Set(protocol.MCPSessionHeader, sessionID)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, resp.Header, err
	}

	prVal := headerValueIgnoreCase(resp.Header, x402.HeaderPaymentRequired)
	var prHeader *x402.X402Payload
	if prVal != "" {
		var parsed x402.X402Payload
		if err := json.Unmarshal([]byte(prVal), &parsed); err == nil {
			prHeader = &parsed
		}
	}

	if resp.StatusCode == http.StatusPaymentRequired {
		return nil, resp.StatusCode, resp.Header, &jsonRPCError{
			Code:                   -32000, // generic error
			Message:                "payment required",
			HTTPStatus:             resp.StatusCode,
			Headers:                flattenHeaders(resp.Header),
			PaymentRequiredHeader:  prHeader,
			PaymentResponseHeader:  headerValueIgnoreCase(resp.Header, x402.HeaderPaymentResponse),
			PaymentRequiredPresent: prVal != "",
		}
	}

	if len(bodyBytes) == 0 {
		return map[string]any{}, resp.StatusCode, resp.Header, nil
	}

	var envelope jsonRPCResponse
	if err := json.Unmarshal(bodyBytes, &envelope); err != nil {
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			bodyText := strings.TrimSpace(string(bodyBytes))
			if bodyText == "" {
				bodyText = http.StatusText(resp.StatusCode)
			}
			return nil, resp.StatusCode, resp.Header, fmt.Errorf("http status %d: %s", resp.StatusCode, bodyText)
		}
		return nil, resp.StatusCode, resp.Header, err
	}
	if envelope.Error != nil {
		return nil, resp.StatusCode, resp.Header, &jsonRPCError{
			Code:                   envelope.Error.Code,
			Message:                envelope.Error.Message,
			HTTPStatus:             resp.StatusCode,
			Headers:                flattenHeaders(resp.Header),
			PaymentRequiredHeader:  prHeader,
			PaymentResponseHeader:  headerValueIgnoreCase(resp.Header, x402.HeaderPaymentResponse),
			PaymentRequiredPresent: prVal != "",
		}
	}
	var raw map[string]any
	if err := json.Unmarshal(bodyBytes, &raw); err != nil {
		return nil, resp.StatusCode, resp.Header, err
	}
	if c.verbose {
		fmt.Printf("[mcp] <- %s (%d)\n", method, resp.StatusCode)
	}
	return raw, resp.StatusCode, resp.Header, nil
}

func (c *Client) startStdio(ctx context.Context) error {
	if c.stdio != nil {
		return nil
	}
	cmdline := strings.TrimSpace(c.endpoint)
	if cmdline == "" {
		cmdline = "dir2mcp"
	}
	parts := strings.Fields(cmdline)
	if len(parts) == 0 {
		return fmt.Errorf("stdio transport requires a command")
	}
	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	c.stdio = &stdioClient{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout)}
	return nil
}

func (c *Client) callStdio(ctx context.Context, method string, params map[string]any, withID bool) (map[string]any, int, http.Header, error) {
	if c.stdio == nil {
		if err := c.startStdio(ctx); err != nil {
			return nil, 0, nil, err
		}
	}
	c.stdio.callMu.Lock()
	defer c.stdio.callMu.Unlock()

	var id *int
	if withID {
		c.mu.Lock()
		n := c.nextID
		c.nextID++
		c.mu.Unlock()
		id = &n
	}
	message := jsonRPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	payload, err := json.Marshal(message)
	if err != nil {
		return nil, 0, nil, err
	}

	c.stdio.mu.Lock()
	if err := writeStdioMessage(c.stdio.stdin, payload); err != nil {
		c.stdio.mu.Unlock()
		return nil, 0, nil, err
	}
	if !withID {
		c.stdio.mu.Unlock()
		return map[string]any{}, http.StatusAccepted, http.Header{}, nil
	}
	stdout := c.stdio.stdout
	c.stdio.mu.Unlock()

	type stdioReadResult struct {
		body []byte
		err  error
	}
	readResultCh := make(chan stdioReadResult, 1)
	go func() {
		body, readErr := readStdioMessage(stdout)
		readResultCh <- stdioReadResult{body: body, err: readErr}
	}()

	var bodyBytes []byte
	select {
	case <-ctx.Done():
		if c.stdio != nil && c.stdio.cmd != nil && c.stdio.cmd.Process != nil {
			_ = c.stdio.cmd.Process.Kill()
		}
		<-readResultCh
		_ = c.Close()
		return nil, 0, nil, ctx.Err()
	case readResult := <-readResultCh:
		if readResult.err != nil {
			return nil, 0, nil, readResult.err
		}
		bodyBytes = readResult.body
	}

	if len(bodyBytes) == 0 {
		return nil, 0, nil, fmt.Errorf("empty stdio response")
	}

	var envelope jsonRPCResponse
	if err := json.Unmarshal(bodyBytes, &envelope); err != nil {
		return nil, 0, nil, err
	}
	if envelope.Error != nil {
		return nil, 0, nil, &jsonRPCError{Code: envelope.Error.Code, Message: envelope.Error.Message}
	}
	var raw map[string]any
	if err := json.Unmarshal(bodyBytes, &raw); err != nil {
		return nil, 0, nil, err
	}
	return raw, http.StatusOK, http.Header{}, nil
}

func writeStdioMessage(w io.Writer, payload []byte) error {
	if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(payload)); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

func readStdioMessage(r *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			break
		}
		parts := strings.SplitN(trimmed, ":", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(parts[0]), "Content-Length") {
			var n int
			if _, err := fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &n); err == nil {
				contentLength = n
			}
		}
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}

func flattenHeaders(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, v := range h {
		out[k] = strings.Join(v, ",")
	}
	return out
}

func headerValueIgnoreCase(h http.Header, name string) string {
	if value := strings.TrimSpace(h.Get(name)); value != "" {
		return value
	}
	for key, values := range h {
		if strings.EqualFold(key, name) {
			return strings.TrimSpace(strings.Join(values, ","))
		}
	}
	return ""
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func asBool(v any) bool {
	b, _ := v.(bool)
	return b
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}
