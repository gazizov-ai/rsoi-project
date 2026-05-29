package domain

import "github.com/google/uuid"

type Hotel struct {
	ID             int64
	UID            uuid.UUID
	Name           string
	Country        string
	City           string
	Address        string
	Stars          *int64
	Price          int64
	TotalRooms     int
	OccupiedRooms  int
	AvailableRooms int
}
