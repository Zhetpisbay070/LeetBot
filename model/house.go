package model

import (
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type House struct {
	ID        primitive.ObjectID `bson:"_id,omitempty"`
	City      string             `bson:"city,omitempty" json:"city"`
	Address   string             `bson:"address,omitempty" json:"address"`
	Phone     string             `bson:"phone,omitempty" json:"phone"`
	RoomCount int                `bson:"room_count,omitempty" json:"room_count"`
	OwnerID   int64              `bson:"owner_id,omitempty" json:"owner_id"`
	Active    bool               `bson:"active,omitempty" json:"active"`
	PhotoIDs  []uuid.UUID        `bson:"photo_ids,omitempty" json:"photo_ids"`
}

func (h House) IsValid() bool {
	return h.City != "" && h.Address != "" && h.Phone != "" && h.RoomCount > 0
}
