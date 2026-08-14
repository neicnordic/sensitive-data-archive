package reader

import (
	"context"
	"errors"
	"io"

	"github.com/neicnordic/sensitive-data-archive/internal/observability"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type tracedReadSeekCloser struct {
	io.ReadSeekCloser
	span trace.Span
}

func (r *tracedReadSeekCloser) Close() error {
	err := r.ReadSeekCloser.Close()
	r.span.End()

	return err
}

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

	seeker, ok := r.(io.ReadSeekCloser)
	if !ok {
		span.End()

		return nil, errors.New("unexpected error: could not cast io.ReadCloser to io.ReadSeekCloser")
	}

	return &tracedReadSeekCloser{
		ReadSeekCloser: seeker,
		span:           span,
	}, nil
}
