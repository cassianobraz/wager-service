package postgres

import (
	"github.com/jackc/pgx/v5/pgtype"
	"uuid"
)

const pgUniqueViolation = "23505"

func toPgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte(id), Valid: id != uuid.Nil()}
}

func fromPgUUID(u pgtype.UUID) uuid.UUID {
	return uuid.UUID(u.Bytes)
}

func toPgUUIDPtr(id *uuid.UUID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return toPgUUID(*id)
}

func fromPgUUIDPtr(u pgtype.UUID) *uuid.UUID {
	if !u.Valid {
		return nil
	}
	id := fromPgUUID(u)
	return &id
}
