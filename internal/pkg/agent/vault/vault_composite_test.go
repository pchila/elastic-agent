// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package vault

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/elastic/elastic-agent/internal/pkg/agent/vault/mocks"
)

//go:generate mockery --name Vault

func TestCompositeVault_Exists(t *testing.T) {
	const key = "test-key"
	vaultError := errors.New("test vault error")

	t.Run("key present in one vault", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		mockVaultNoKey := mocks.NewVault(t)
		// this vault does not contain the key
		mockVaultNoKey.EXPECT().Exists(ctx, key).Return(false, nil).Once()

		mockVaultError := mocks.NewVault(t)
		// this vault always return error
		mockVaultError.EXPECT().Exists(ctx, key).Return(false, vaultError).Once()

		mockVaultKeyPresent := mocks.NewVault(t)
		// this vault contains the key
		mockVaultKeyPresent.EXPECT().Exists(ctx, key).Return(true, nil).Once()

		cv := NewCompositeVault([]Vault{mockVaultNoKey, mockVaultError, mockVaultKeyPresent})

		exists, err := cv.Exists(ctx, key)
		assert.True(t, exists, "composite vault should find the key in one of the vault it contains")
		assert.NoError(t, err, "if we find the key in one of the vaults we ignore the errors from some other vault")
		assert.Equal(t, 2, cv.lastSuccessfulVault, "composite vault should save the index of the successful vault")
	})

	t.Run("key not present in one vault, error for the other", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		mockVaultNoKey := mocks.NewVault(t)
		// this vault does not contain the key
		mockVaultNoKey.EXPECT().Exists(ctx, key).Return(false, nil).Once()

		mockVaultError := mocks.NewVault(t)
		// this vault always return error
		mockVaultError.EXPECT().Exists(ctx, key).Return(false, vaultError).Once()

		cv := NewCompositeVault([]Vault{mockVaultNoKey, mockVaultError})

		exists, err := cv.Exists(ctx, key)
		assert.False(t, exists, "composite vault should return false if no vault could find the key")
		assert.ErrorIs(t, err, vaultError, "if we don't find the key in one of the vaults we return the errors")
		assert.Equal(t, noLastSuccessfulVault, cv.lastSuccessfulVault, "composite vault should not update the index of the successful vault for failures")
	})

	t.Run("key found in the second vault, next operation should start directly from the last successful one", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		mockVaultNoKey := mocks.NewVault(t)
		// this vault does not contain the key
		mockVaultNoKey.EXPECT().Exists(ctx, key).Return(false, nil).Once()

		mockVaultKeyPresent := mocks.NewVault(t)
		// this vault contains the key
		mockVaultKeyPresent.EXPECT().Exists(ctx, key).Return(true, nil).Twice()

		cv := NewCompositeVault([]Vault{mockVaultNoKey, mockVaultKeyPresent})

		// first operation, we start from the beginning
		exists, err := cv.Exists(ctx, key)
		assert.True(t, exists, "composite vault should find the key in one of the vault it contains")
		assert.NoError(t, err, "if we find the key in one of the vaults we ignore the errors from some other vault")
		assert.Equal(t, 1, cv.lastSuccessfulVault, "composite vault should save the index of the successful vault")

		// second operation, we should start from the last successful (see the different number invocations on the mocks above)
		exists, err = cv.Exists(ctx, key)
		assert.True(t, exists, "composite vault should find the key in one of the vault it contains")
		assert.NoError(t, err, "if we find the key in one of the vaults we ignore the errors from some other vault")
		assert.Equal(t, 1, cv.lastSuccessfulVault, "composite vault should save the index of the successful vault")
	})
}
