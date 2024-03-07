// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package vault

import (
	"context"
	"errors"
)

const noLastSuccessfulVault = -1

type CompositeVault struct {
	vaults              []Vault
	lastSuccessfulVault int
}

func (c *CompositeVault) Exists(ctx context.Context, key string) (bool, error) {
	startIndex := 0
	if c.lastSuccessfulVault > noLastSuccessfulVault {
		startIndex = c.lastSuccessfulVault
	}
	var err error
	for i := startIndex; i < startIndex+len(c.vaults); i++ {
		exists, vErr := c.vaults[i%len(c.vaults)].Exists(ctx, key)
		if vErr != nil {
			err = errors.Join(err, vErr)
			continue
		}
		if exists {
			// we found the key, set lastSuccessfulVault and return
			c.lastSuccessfulVault = i % len(c.vaults)
			return exists, nil
		}
	}
	return false, err
}

func (c *CompositeVault) Get(ctx context.Context, key string) (dec []byte, err error) {
	//TODO implement me
	panic("implement me")
}

func (c *CompositeVault) Set(ctx context.Context, key string, data []byte) (err error) {
	//TODO implement me
	panic("implement me")
}

func (c *CompositeVault) Remove(ctx context.Context, key string) (err error) {
	//TODO implement me
	panic("implement me")
}

func (c *CompositeVault) Close() error {
	//TODO implement me
	panic("implement me")
}

func NewCompositeVault(vaults []Vault) *CompositeVault {
	return &CompositeVault{
		vaults:              vaults,
		lastSuccessfulVault: noLastSuccessfulVault,
	}
}
