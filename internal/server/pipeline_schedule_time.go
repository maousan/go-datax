package server

import (
	"fmt"
	"time"

	"github.com/longkeyy/go-datax/internal/quartzcron"
)

type pipelineScheduleTiming struct {
	spec *quartzcron.Schedule
	loc  *time.Location
}

func parsePipelineScheduleTiming(cronExpr, timezone string) (*pipelineScheduleTiming, error) {
	spec, err := quartzcron.Parse(cronExpr)
	if err != nil {
		return nil, fmt.Errorf("cron 表达式不正确: %v", err)
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("时区不正确: %v", err)
	}
	return &pipelineScheduleTiming{spec: spec, loc: loc}, nil
}

func (t *pipelineScheduleTiming) next(now time.Time) time.Time {
	return t.spec.Next(now.In(t.loc))
}
