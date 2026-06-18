package handler

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/http"

	"github.com/gin-gonic/gin"
)

type PartnerHandler struct {
	db *sql.DB
}

func NewPartnerHandler(db *sql.DB) *PartnerHandler {
	return &PartnerHandler{db: db}
}

// Invite godoc
// @Summary      Generate invite code
// @Description  Generates a random invite code for partner linking
// @Tags         partner
// @Security     BearerAuth
// @Produce      json
// @Success      200  {object}  map[string]string
// @Failure      400  {object}  map[string]string
// @Failure      401  {object}  map[string]string
// @Router       /partner/invite [post]
func (h *PartnerHandler) Invite(c *gin.Context) {
	userID := c.GetInt64("userID")

	var partnerID *int64
	err := h.db.QueryRow("SELECT partner_id FROM users WHERE id = ?", userID).Scan(&partnerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if partnerID != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "already have a partner"})
		return
	}

	b := make([]byte, 4)
	rand.Read(b)
	code := hex.EncodeToString(b)

	_, err = h.db.Exec("UPDATE users SET invite_code = ? WHERE id = ?", code, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"invite_code": code})
}

// Accept godoc
// @Summary      Accept invite code
// @Description  Links two users as partners using an invite code
// @Tags         partner
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        body  body      map[string]string  true  "Invite code"
// @Success      200   {object}  map[string]interface{}
// @Failure      400   {object}  map[string]string
// @Failure      404   {object}  map[string]string
// @Router       /partner/accept [post]
func (h *PartnerHandler) Accept(c *gin.Context) {
	userID := c.GetInt64("userID")

	var req struct {
		InviteCode string `json:"invite_code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var partnerID *int64
	err := h.db.QueryRow("SELECT partner_id FROM users WHERE id = ?", userID).Scan(&partnerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if partnerID != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "already have a partner"})
		return
	}

	var inviterID int64
	var inviterUsername string
	var inviterPartnerID *int64
	err = h.db.QueryRow(
		"SELECT id, username, partner_id FROM users WHERE invite_code = ?", req.InviteCode,
	).Scan(&inviterID, &inviterUsername, &inviterPartnerID)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "invalid invite code"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if inviterPartnerID != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "inviter already has a partner"})
		return
	}
	if inviterID == userID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot partner with yourself"})
		return
	}

	tx, err := h.db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec("UPDATE users SET partner_id = ?, invite_code = '' WHERE id = ?", inviterID, userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if _, err := tx.Exec("UPDATE users SET partner_id = ?, invite_code = '' WHERE id = ?", userID, inviterID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"partner_id":   inviterID,
		"partner_name": inviterUsername,
	})
}

// Get godoc
// @Summary      Get partner info
// @Description  Returns the current partner's info or 404
// @Tags         partner
// @Security     BearerAuth
// @Produce      json
// @Success      200  {object}  map[string]interface{}
// @Failure      404  {object}  map[string]string
// @Router       /partner [get]
func (h *PartnerHandler) Get(c *gin.Context) {
	userID := c.GetInt64("userID")

	var partnerID *int64
	err := h.db.QueryRow("SELECT partner_id FROM users WHERE id = ?", userID).Scan(&partnerID)
	if err != nil || partnerID == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no partner"})
		return
	}

	var username string
	err = h.db.QueryRow("SELECT username FROM users WHERE id = ?", *partnerID).Scan(&username)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "partner not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"partner_id":   *partnerID,
		"partner_name": username,
	})
}

// Disconnect godoc
// @Summary      Disconnect partner
// @Description  Removes partner link from both users and deletes shared shopping lists
// @Tags         partner
// @Security     BearerAuth
// @Success      204
// @Failure      404  {object}  map[string]string
// @Router       /partner [delete]
func (h *PartnerHandler) Disconnect(c *gin.Context) {
	userID := c.GetInt64("userID")

	var partnerID *int64
	err := h.db.QueryRow("SELECT partner_id FROM users WHERE id = ?", userID).Scan(&partnerID)
	if err != nil || partnerID == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no partner"})
		return
	}

	tx, err := h.db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec("UPDATE users SET partner_id = NULL WHERE id IN (?, ?)", userID, *partnerID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// delete shared shopping lists for both users
	if _, err := tx.Exec("DELETE FROM shopping_lists WHERE shared = 1 AND user_id IN (?, ?)", userID, *partnerID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusNoContent, nil)
}
