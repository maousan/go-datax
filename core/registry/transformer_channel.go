package plugin

import (
	"fmt"

	"github.com/longkeyy/go-datax/common/element"
	"github.com/longkeyy/go-datax/common/logger"
	"github.com/longkeyy/go-datax/common/plugin"
	"github.com/longkeyy/go-datax/common/statistics"
	"github.com/longkeyy/go-datax/common/transformer"
	"go.uber.org/zap"
)

// TransformerChannel applies a transformer chain before records reach the
// underlying channel and records DataX/Addax style transform metrics:
// input/output records & bytes, dirty records & bytes, filters and used time.
//
// A record becomes dirty as soon as one transformer fails. The remaining
// transformers are skipped and the record is dropped (Push returns nil), unless
// the configured errorLimit is exceeded, in which case the error is returned to
// abort the task group.
type TransformerChannel struct {
	plugin.Channel
	executions    []*transformer.Execution
	communication *statistics.Communication
	errorLimiter  *statistics.ErrorLimiter
}

// NewTransformerChannel wraps channel with a transformer chain. communication
// and errorLimiter may be nil, in which case metrics/limit checks are skipped.
func NewTransformerChannel(
	channel plugin.Channel,
	executions []*transformer.Execution,
	communication *statistics.Communication,
	errorLimiter *statistics.ErrorLimiter,
) *TransformerChannel {
	return &TransformerChannel{
		Channel:       channel,
		executions:    executions,
		communication: communication,
		errorLimiter:  errorLimiter,
	}
}

// Push applies the transformer chain and forwards surviving records.
func (c *TransformerChannel) Push(record element.Record) error {
	if c.Channel.IsClosed() {
		return ErrChannelClosed
	}
	if len(c.executions) == 0 {
		return c.Channel.Push(record)
	}

	if c.errorLimiter != nil {
		c.errorLimiter.AddTotalRecord(1)
	}
	c.increaseCounter(statistics.TRANSFORMER_INPUT_RECORDS, 1)
	c.increaseCounter(statistics.TRANSFORMER_INPUT_BYTES, int64(record.GetByteSize()))

	current := record
	for _, execution := range c.executions {
		result, err := execution.Evaluate(current)

		used := execution.LastUsedNanos()
		c.increaseCounter(statistics.TRANSFORMER_USED_TIME, used)
		c.increaseCounter(statistics.TRANSFORMER_NAME_PREFIX+execution.Name(), used)

		if err != nil {
			dirtyBytes := int64(current.GetByteSize())
			c.increaseCounter(statistics.TRANSFORMER_FAILED_RECORDS, 1)
			c.increaseCounter(statistics.TRANSFORMER_DIRTY_RECORDS, 1)
			c.increaseCounter(statistics.TRANSFORMER_DIRTY_BYTES, dirtyBytes)

			fields := []zap.Field{
				zap.String("transformer", execution.Name()),
				zap.Error(err),
			}
			if columnIndex := execution.ColumnIndex(); columnIndex >= 0 {
				fields = append(fields, zap.Int("columnIndex", columnIndex))
			}
			logger.Component().WithComponent("Transformer").Debug("Dirty record dropped", fields...)

			if c.errorLimiter != nil {
				if limitErr := c.errorLimiter.AddErrorRecord(1); limitErr != nil {
					return fmt.Errorf("transformer %s produced dirty data: %v; %w", execution.Name(), err, limitErr)
				}
			}
			// Skip remaining transformers and drop this record.
			return nil
		}

		if result == nil {
			// Filtered: skip remaining transformers and drop this record.
			c.increaseCounter(statistics.TRANSFORMER_FILTER_RECORDS, 1)
			if c.errorLimiter != nil {
				c.errorLimiter.AddFilterRecord(1)
			}
			return nil
		}

		current = result
	}

	c.increaseCounter(statistics.TRANSFORMER_SUCCEED_RECORDS, 1)
	c.increaseCounter(statistics.TRANSFORMER_OUTPUT_RECORDS, 1)
	c.increaseCounter(statistics.TRANSFORMER_OUTPUT_BYTES, int64(current.GetByteSize()))

	return c.Channel.Push(current)
}

func (c *TransformerChannel) increaseCounter(key string, delta int64) {
	if c.communication != nil {
		c.communication.IncreaseCounter(key, delta)
	}
}
