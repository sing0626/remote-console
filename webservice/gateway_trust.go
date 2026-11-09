package webservice

import (
	"bytes"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"net/url"
	"remoteconsole.local/connect/trust"
	"time"
)

type gatewayIdentity struct {
	origin      *url.URL
	fingerprint string
	key         []byte
	client      *http.Client
	verifier    trust.Verifier
}

// ConfigureGatewayTrust receives only the identity already held by this
// enrolled phone. A controller never receives the tunnel/delegation key.
func (wm *WebMaster) ConfigureGatewayTrust(origin, fingerprint string, key []byte, transport http.RoundTripper) error {
	u, e := url.Parse(origin)
	if e != nil {
		return e
	}
	if u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(key) != 32 {
		return &url.Error{Op: "configure", URL: "gateway", Err: http.ErrNotSupported}
	}
	u.Path = ""
	identity := &gatewayIdentity{origin: u, fingerprint: fingerprint, key: append([]byte(nil), key...), client: &http.Client{Transport: transport, Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	wm.gatewayMu.Lock()
	wm.gatewayIdentity = identity
	wm.gatewayMu.Unlock()
	return nil
}
func (wm *WebMaster) gateway() *gatewayIdentity {
	wm.gatewayMu.RLock()
	defer wm.gatewayMu.RUnlock()
	return wm.gatewayIdentity
}
func (wm *WebMaster) authenticateGateway(c *gin.Context) bool {
	token := c.GetHeader(trust.DelegationHeader)
	if token == "" {
		return false
	}
	identity := wm.gateway()
	if identity == nil {
		c.AbortWithStatusJSON(401, gin.H{"error": "gateway_not_configured"})
		return true
	}
	body, e := io.ReadAll(io.LimitReader(c.Request.Body, 4*1024*1024+1))
	if e != nil || len(body) > 4*1024*1024 {
		c.AbortWithStatusJSON(400, gin.H{"error": "invalid_request"})
		return true
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	subject, e := identity.verifier.Verify(identity.key, "delegate", identity.fingerprint, token, c.Request, body, time.Now())
	if e != nil {
		c.AbortWithStatusJSON(401, gin.H{"error": "invalid_gateway_authorization"})
		return true
	}
	c.Set("gateway_controller", subject)
	c.Next()
	return true
}
func (wm *WebMaster) handleGatewayTrustStatus(c *gin.Context) {
	identity := wm.gateway()
	result := gin.H{"configured": identity != nil, "scope": "all_phones"}
	if identity != nil {
		result["origin"] = identity.origin.String()
	}
	c.JSON(200, result)
}
func (wm *WebMaster) handleGatewayTrustList(c *gin.Context)    { wm.gatewayOwnerRequest(c, "list") }
func (wm *WebMaster) handleGatewayTrustApprove(c *gin.Context) { wm.gatewayOwnerRequest(c, "approve") }
func (wm *WebMaster) handleGatewayTrustRevoke(c *gin.Context)  { wm.gatewayOwnerRequest(c, "revoke") }
func (wm *WebMaster) gatewayOwnerRequest(c *gin.Context, operation string) {
	// Approval is authenticated by the receiver App's local owner session.
	// A delegated gateway request is not an owner approval request.
	if c.GetString("gateway_controller") != "" {
		c.AbortWithStatusJSON(403, gin.H{"error": "phone_owner_required"})
		return
	}
	identity := wm.gateway()
	if identity == nil {
		c.JSON(503, gin.H{"error": "gateway_not_configured"})
		return
	}
	body, e := io.ReadAll(io.LimitReader(c.Request.Body, 4097))
	if e != nil || len(body) > 4096 {
		c.JSON(400, gin.H{"error": "invalid_request"})
		return
	}
	target := *identity.origin
	target.Path = "/_remoteconsole/trust/owner/" + operation
	request, e := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, target.String(), bytes.NewReader(body))
	if e != nil {
		c.JSON(502, gin.H{"error": "gateway_unavailable"})
		return
	}
	request.Header.Set("Content-Type", "application/json")
	token, e := trust.Sign(identity.key, "owner", identity.fingerprint, "phone-owner", request, body, time.Now())
	if e != nil {
		c.JSON(502, gin.H{"error": "gateway_unavailable"})
		return
	}
	request.Header.Set(trust.OwnerHeader, identity.fingerprint+":"+token)
	response, e := identity.client.Do(request)
	if e != nil {
		c.JSON(502, gin.H{"error": "gateway_unavailable"})
		return
	}
	defer response.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(response.Body, 128*1024+1))
	if e != nil || len(raw) > 128*1024 {
		c.JSON(502, gin.H{"error": "gateway_unavailable"})
		return
	}
	var object map[string]any
	if json.Unmarshal(raw, &object) != nil {
		c.JSON(502, gin.H{"error": "gateway_update_required"})
		return
	}
	c.JSON(response.StatusCode, object)
}
