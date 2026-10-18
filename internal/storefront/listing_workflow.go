package storefront

import (
	"context"
	"encoding/json"
)

type Screening interface {
	Moderate(context.Context, []byte, string, string) (bool, error)
	Resize(context.Context, []byte, string, string, string) (json.RawMessage, error)
}

type ListingSubmission struct {
	OrderID   string
	Caption   string
	Filename  string
	MediaType string
	Image     []byte
}

type OrderUpdate struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

type OrderState struct {
	OrderID         string          `json:"order_id"`
	Listing         string          `json:"listing"`
	Checkout        string          `json:"checkout"`
	Fulfillment     string          `json:"fulfillment"`
	Receipt         string          `json:"receipt"`
	CustomerUpdates []OrderUpdate   `json:"customer_updates"`
	ProcessedImage  json.RawMessage `json:"processed_image,omitempty"`
}

type ListingWorkflow struct {
	screening Screening
}

func NewListingWorkflow(screening Screening) *ListingWorkflow {
	return &ListingWorkflow{screening: screening}
}

func (w *ListingWorkflow) Submit(ctx context.Context, submission ListingSubmission) (OrderState, error) {
	flagged, err := w.screening.Moderate(ctx, submission.Image, submission.MediaType, submission.Caption)
	if err != nil {
		return OrderState{}, err
	}
	if flagged {
		return OrderState{
			OrderID: submission.OrderID,
			Listing: "quarantined", Checkout: "authorized", Fulfillment: "held", Receipt: "pending",
			CustomerUpdates: []OrderUpdate{{Kind: "review", Message: "Your listing is queued for review."}},
		}, nil
	}

	processed, err := w.screening.Resize(ctx, submission.Image, submission.Filename, submission.MediaType, "listing:"+submission.OrderID)
	if err != nil {
		return OrderState{}, err
	}
	return OrderState{
		OrderID: submission.OrderID,
		Listing: "published", Checkout: "captured", Fulfillment: "queued", Receipt: "issued",
		CustomerUpdates: []OrderUpdate{
			{Kind: "listing", Message: "Your listing is published."},
			{Kind: "order", Message: "Your order is queued for fulfillment."},
		},
		ProcessedImage: processed,
	}, nil
}
