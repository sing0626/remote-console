package webservice

import (
	"crypto/subtle"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const pairingMaxAttempts = 5
const pairingCooldown = 10 * time.Minute

type UnlockAttemptRecord struct {
	Attempts  int
	IsLocked  bool
	LockUntil time.Time
}

func respondPairingLocked(c *gin.Context, until, now time.Time) {
	seconds := int((until.Sub(now) + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	c.Header("Retry-After", strconv.Itoa(seconds))
	c.JSON(http.StatusTooManyRequests, gin.H{"result": "failed", "code": "pin_locked", "message": "Too many attempts, please try again later", "leftTries": 0, "lockUntil": until, "retryAfterSeconds": seconds})
}

func (wm *WebMaster) handleUnlock(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
	var req struct {
		PIN string `json:"pin"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"result": "error", "message": "Invalid request"})
		return
	}
	req.PIN = strings.TrimSpace(req.PIN)
	ip, now := c.ClientIP(), time.Now()
	wm.unlockMu.Lock()
	defer wm.unlockMu.Unlock()
	record, exists := wm.UnlockAttemptRecords[ip]
	// Reset expired windows even when the fifth failure happened before a sixth request.
	if exists && !record.LockUntil.IsZero() && !now.Before(record.LockUntil) {
		delete(wm.UnlockAttemptRecords, ip)
		record, exists = UnlockAttemptRecord{}, false
	}
	if !exists && len(wm.UnlockAttemptRecords) >= 1024 {
		for key, item := range wm.UnlockAttemptRecords {
			if !now.Before(item.LockUntil) {
				delete(wm.UnlockAttemptRecords, key)
			}
		}
		if len(wm.UnlockAttemptRecords) >= 1024 {
			c.Header("Retry-After", "60")
			c.JSON(http.StatusTooManyRequests, gin.H{"result": "failed", "code": "busy", "retryAfterSeconds": 60})
			return
		}
	}
	if record.IsLocked {
		respondPairingLocked(c, record.LockUntil, now)
		return
	}
	if wm.pin == "" || subtle.ConstantTimeCompare([]byte(req.PIN), []byte(wm.pin)) != 1 {
		record.Attempts++
		if record.LockUntil.IsZero() {
			record.LockUntil = now.Add(pairingCooldown)
		}
		if record.Attempts >= pairingMaxAttempts {
			record.IsLocked = true
			record.LockUntil = now.Add(pairingCooldown)
			wm.UnlockAttemptRecords[ip] = record
			respondPairingLocked(c, record.LockUntil, now)
			return
		}
		wm.UnlockAttemptRecords[ip] = record
		c.JSON(http.StatusOK, gin.H{"result": "failed", "code": "pin_incorrect", "message": "Incorrect PIN", "leftTries": pairingMaxAttempts - record.Attempts})
		return
	}
	token, err := wm.GenerateToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Token Generation Failed"})
		return
	}
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie("auth_token", token, 3600*2, "/", "", secureCookieRequest(c.Request), true)
	c.JSON(http.StatusOK, gin.H{"result": "success", "message": "Unlocked", "token": token})
	delete(wm.UnlockAttemptRecords, ip)
}
