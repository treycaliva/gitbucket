package apps

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
)

// CollectionWebhookDeliveries is the Firestore collection name for the
// outbound webhook delivery audit log.
const CollectionWebhookDeliveries = "webhook_deliveries"

// WebhookDelivery is the persisted record for one fan-out attempt.
type WebhookDelivery struct {
	DeliveryID       string    `firestore:"delivery_id"`
	AppID            string    `firestore:"app_id"`
	InstallationID   string    `firestore:"installation_id"`
	Event            string    `firestore:"event"`
	PayloadSHA256    string    `firestore:"payload_sha256"`
	TargetURL        string    `firestore:"target_url"`
	Status           string    `firestore:"status"`
	Attempts         int       `firestore:"attempts"`
	LastResponseCode int       `firestore:"last_response_code"`
	LastAttemptedAt  time.Time `firestore:"last_attempted_at"`
	CreatedAt        time.Time `firestore:"created_at"`
}

// CreateDeliveryInput is the subset of fields the caller supplies.
type CreateDeliveryInput struct {
	AppID          string
	InstallationID string
	Event          string
	TargetURL      string
	PayloadSHA256  string
}

// DeliveryUpdate carries the fields the dispatcher writes back after each
// HTTPS attempt.
type DeliveryUpdate struct {
	Status           string
	Attempts         int
	LastResponseCode int
	LastAttemptedAt  time.Time
}

// CreateDelivery writes a new delivery doc with status=pending and returns
// the persisted record.
func CreateDelivery(ctx context.Context, fs *firestore.Client, in CreateDeliveryInput) (*WebhookDelivery, error) {
	if fs == nil {
		return nil, fmt.Errorf("firestore client is nil")
	}
	idBytes := make([]byte, 12)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, fmt.Errorf("generate delivery_id: %w", err)
	}
	rec := &WebhookDelivery{
		DeliveryID:     hex.EncodeToString(idBytes),
		AppID:          in.AppID,
		InstallationID: in.InstallationID,
		Event:          in.Event,
		PayloadSHA256:  in.PayloadSHA256,
		TargetURL:      in.TargetURL,
		Status:         "pending",
		Attempts:       0,
		CreatedAt:      time.Now().UTC(),
	}
	if _, err := fs.Collection(CollectionWebhookDeliveries).Doc(rec.DeliveryID).Create(ctx, rec); err != nil {
		return nil, fmt.Errorf("write delivery: %w", err)
	}
	return rec, nil
}

// GetDelivery returns the delivery record or (nil, nil) on not-found.
func GetDelivery(ctx context.Context, fs *firestore.Client, deliveryID string) (*WebhookDelivery, error) {
	doc, err := fs.Collection(CollectionWebhookDeliveries).Doc(deliveryID).Get(ctx)
	if err != nil {
		if isFirestoreNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var d WebhookDelivery
	if err := doc.DataTo(&d); err != nil {
		return nil, err
	}
	return &d, nil
}

// UpdateDeliveryStatus updates the mutable fields on an existing delivery
// after a dispatch attempt.
func UpdateDeliveryStatus(ctx context.Context, fs *firestore.Client, deliveryID string, u DeliveryUpdate) error {
	_, err := fs.Collection(CollectionWebhookDeliveries).Doc(deliveryID).Update(ctx, []firestore.Update{
		{Path: "status", Value: u.Status},
		{Path: "attempts", Value: u.Attempts},
		{Path: "last_response_code", Value: u.LastResponseCode},
		{Path: "last_attempted_at", Value: u.LastAttemptedAt},
	})
	return err
}
