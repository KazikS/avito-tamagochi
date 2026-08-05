package handlers

import (
	"net/http"
	"tamagochi/backend/internal/usecase"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type PetHandler struct {
	uc usecase.PetService
}


func (h *PetHandler) GetPetInfo(c *gin.Context) {
	select {
	case <-c.Request.Context().Done():
		return
	default:
	}
	
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	pet, err := h.uc.GetPetState(c.Request.Context(), id)
	
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "pet not found"})
		return
	}
	c.JSON(http.StatusOK, pet)
}
