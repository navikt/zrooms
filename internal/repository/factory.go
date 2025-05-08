// Package repository provides the initialization for repository implementations
package repository

import (
	"github.com/navikt/zrooms/internal/config"
	"github.com/navikt/zrooms/internal/repository/memory"
	"github.com/navikt/zrooms/internal/repository/redis"
)

// init registers the actual repository implementations
func init() {
	// Register the Redis repository constructor
	newRedisRepository = func(cfg config.RedisConfig) (Repository, error) {
		return redis.NewRepository(cfg)
	}

	// Register the memory repository constructor
	newMemoryRepository = func() Repository {
		return memory.NewRepository()
	}
}
