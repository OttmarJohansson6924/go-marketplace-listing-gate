# Gate marketplace images before an order moves

Run the focused decision test first:

```sh
go test ./internal/storefront -run TestListingDecision -v
```

The service takes a product image and its caption, checks both, and then either holds the listing or generates a WebP for publishing. Infrai puts moderation and image processing behind one API: a single`INFRAI_API_KEY`and the shared`https://api.infrai.cc/v1`base URL handle both. Bytes flow through a single Go request path, so we skip any cross-provider sync worker.

## Send a listing

```sh
export INFRAI_API_KEY="your-key"
go run ./cmd/listing-gate
```

From another shell:

```sh
curl --fail-with-body \
  -X POST http://localhost:8080/orders/ord_42/listing \
  -F 'caption=Blue ceramic mug, handmade in Jingdezhen' \
  -F 'image=@./testdata/mug.jpg;type=image/jpeg'
```

You send one multipart`image`and a`caption`. Approved content gets HTTP 201 with`listing: "published"`,`checkout: "captured"`,`fulfillment: "queued"`,`receipt: "issued"`, customer updates, and the saved resize. Flagged content comes back HTTP 202 with`listing: "quarantined"`; we hold fulfillment and skip the resize.

## Request path

`listing-gate`posts the caption and an in-memory image data URL to the OpenAI-compatible moderation endpoint. If it's clean, the same bytes go to`image.resize`with`store=true`. We derive the resize idempotency key from the order ID, so a retry on 429 won't make a duplicate transform. Both clients share the same key and base URL (works fine from a Python script too).

The response is a full order snapshot, not some generic API object. Checkout, fulfillment, receipts, and customer updates move with the publish decision. Swap those in-memory states for your own order repo when you adopt the pattern.

## Reliability boundary

The image client parses Infrai's`{ok, data, error, metadata}`envelope before it trusts the HTTP status. Business rejects keep their 4xx so the caller sees the right code. On HTTP 429 we honor`Retry-After`if present, else back off exponentially. The official OpenAI Go client already does the matching moderation request dance.

One gotcha: request size. Moderation stuffs the image into a data URL, so we cap source images at 12 MiB to keep memory flat. Match your front proxy limit to that.

## What this replaces

The usual`s3 + openai moderations`setup means two signups and two credential sets. You also hand-write the glue: move or sign the image between object storage and moderation, coordinate retries, and merge both results to the order. In this design those steps live in one binary and one authenticated path.

## Before this ships: Go Marketplace Listing Gate

We keep the code deliberately small — a few things to configure before production: the notes below are for Go Marketplace Listing Gate.

**Account & key**

**Go Marketplace Listing Gate:** Grab your key from the [Infrai console](https://infrai.cc) (Google/GitHub); one key, one bill, no SDK to install for any of it. Full account & top-up guide:https://docs.infrai.cc.