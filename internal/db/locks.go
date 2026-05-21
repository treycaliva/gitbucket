package db

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// LeasedLock represents a distributed leased lock stored in Firestore.
type LeasedLock struct {
	LockedBy   string    `firestore:"lockedBy"`
	AcquiredAt time.Time `firestore:"acquiredAt"`
	ExpiresAt  time.Time `firestore:"expiresAt"`
}

// AcquireLeasedLock attempts to acquire a leased lock on a repository using transaction ReadTime for clock-drift immunity.
func AcquireLeasedLock(ctx context.Context, client *firestore.Client, owner, repo, workerID string, leaseDuration time.Duration) error {
	if client == nil {
		return fmt.Errorf("firestore client is nil")
	}
	lockId := fmt.Sprintf("%s_%s", owner, repo)
	lockRef := client.Collection("locks").Doc(lockId)

	return client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(lockRef)
		var now time.Time

		if err != nil {
			if status.Code(err) == codes.NotFound {
				// No lock exists, initialize using transaction ReadTime
				now = time.Now()
				if doc != nil && !doc.ReadTime.IsZero() {
					now = doc.ReadTime
				}
				newLock := LeasedLock{
					LockedBy:   workerID,
					AcquiredAt: now,
					ExpiresAt:  now.Add(leaseDuration),
				}
				return tx.Set(lockRef, newLock)
			}
			return err
		}

		// Use the Firestore-provided server ReadTime to completely eliminate local container clock drift
		now = doc.ReadTime
		if now.IsZero() {
			now = time.Now()
		}

		var existingLock LeasedLock
		if err := doc.DataTo(&existingLock); err != nil {
			return err
		}

		// Check if the lock is active or expired
		if now.Before(existingLock.ExpiresAt) {
			// Lock is active, return conflict
			return fmt.Errorf("lock is active and held by %s until %s", existingLock.LockedBy, existingLock.ExpiresAt)
		}

		// Lock has expired; steal it using server ReadTime as the new baseline
		stolenLock := LeasedLock{
			LockedBy:   workerID,
			AcquiredAt: now,
			ExpiresAt:  now.Add(leaseDuration),
		}
		return tx.Set(lockRef, stolenLock)
	})
}

// ReleaseLeasedLock releases the lock if it belongs to workerID.
func ReleaseLeasedLock(ctx context.Context, client *firestore.Client, owner, repo, workerID string) error {
	if client == nil {
		return fmt.Errorf("firestore client is nil")
	}
	lockId := fmt.Sprintf("%s_%s", owner, repo)
	lockRef := client.Collection("locks").Doc(lockId)

	return client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(lockRef)
		if err != nil {
			if status.Code(err) == codes.NotFound {
				return nil
			}
			return err
		}

		var existingLock LeasedLock
		if err := doc.DataTo(&existingLock); err != nil {
			return err
		}

		// Only release if the lock is still ours
		if existingLock.LockedBy == workerID {
			return tx.Delete(lockRef)
		}
		return errors.New("cannot release lock owned by another worker")
	})
}

// RenewLeasedLock extends the lease of the lock if owned by workerID.
func RenewLeasedLock(ctx context.Context, client *firestore.Client, owner, repo, workerID string, extension time.Duration) error {
	if client == nil {
		return fmt.Errorf("firestore client is nil")
	}
	lockId := fmt.Sprintf("%s_%s", owner, repo)
	lockRef := client.Collection("locks").Doc(lockId)

	return client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(lockRef)
		if err != nil {
			return err
		}

		var l LeasedLock
		if err := doc.DataTo(&l); err != nil {
			return err
		}

		// Verify this worker still owns the lock before renewing
		if l.LockedBy != workerID {
			return errors.New("lock ownership lost; stolen by another worker")
		}

		// Extend expiry based on the server read time
		now := doc.ReadTime
		if now.IsZero() {
			now = time.Now()
		}
		l.ExpiresAt = now.Add(extension)

		return tx.Set(lockRef, l)
	})
}

// StartLockHeartbeat spawns a background goroutine to periodically extend the lock's expiresAt field.
// If a lock renewal fails, it triggers context cancellation of the parent context via the provided CancelFunc.
// It returns a stop channel to signal completion and shutdown.
func StartLockHeartbeat(ctx context.Context, cancel context.CancelFunc, client *firestore.Client, owner, repo, workerID string, interval time.Duration, extension time.Duration) chan struct{} {
	stopChan := make(chan struct{})

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				err := RenewLeasedLock(ctx, client, owner, repo, workerID, extension)
				if err != nil {
					log.Printf("[Sync Heartbeat] Lock renewal failed for %s/%s: %v. Aborting task and canceling context.", owner, repo, err)
					cancel() // Trigger context cancellation of the parent context immediately
					return
				}
			case <-stopChan:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	return stopChan
}
