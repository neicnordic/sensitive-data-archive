package reader

import (
	"context"
	"errors"
	"io"

	"github.com/neicnordic/sensitive-data-archive/internal/observability"
	"go.opentelemetry.io/otel/attribute"
)

func (reader *Reader) NewFileReadSeeker(ctx context.Context, location, filePath string) (io.ReadSeekCloser, error) {
	ctx, span := observability.StartSpan(ctx, "storage.posix.reader.NewFileReadSeeker",
		attribute.String("location", location),
		attribute.String("filePath", filePath),
	)

	r, err := reader.NewFileReader(ctx, location, filePath)
	if err != nil {
		span.End()

		return nil, err
	}

	seeker, ok := r.(*tracedReadSeekCloser)
	if !ok {
		span.End()

		return nil, errors.New("unexpected error: could not cast io.ReadCloser to io.ReadSeekCloser")
	}
	seeker.span = span

	return seeker, nil
}
