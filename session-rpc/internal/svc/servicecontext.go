package svc

import (
	"awesomeProject/session-rpc/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ServiceContext struct {
	Config config.Config
	DB     *pgxpool.Pool
}

func NewServiceContext(c config.Config, db *pgxpool.Pool) *ServiceContext {
	return &ServiceContext{
		Config: c,
		DB:     db,
	}
}
