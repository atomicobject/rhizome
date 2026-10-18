package store

import (
	"context"
	"database/sql"
)

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

var schemaMigrations = []func(context.Context, execer) error{
	intelBaselineStep00,
	intelBaselineStep01,
	intelBaselineStep02,
	intelBaselineStep03,
	intelBaselineStep04,
	intelBaselineStep05,
	intelBaselineStep06,
	intelBaselineStep07,
	intelBaselineStep08,
	intelBaselineStep09,
	intelBaselineStep10,
	intelBaselineStep11,
	intelBaselineStep12,
	intelBaselineStep13,
	intelBaselineStep14,
	intelBaselineStep15,
	intelBaselineStep16,
	intelBaselineStep17,
	intelBaselineStep18,
	intelBaselineStep19,
	intelBaselineStep20,
	intelBaselineStep21,
	intelBaselineStep22,
	intelBaselineStep23,
	intelBaselineStep24,
	intelBaselineStep25,
	intelBaselineStep26,
	intelBaselineStep27,
	intelBaselineStep28,
	intelBaselineStep29,
	intelBaselineStep30,
	intelBaselineStep31,
	intelBaselineStep32,
	intelBaselineStep33,
	intelBaselineStep34,
	intelBaselineStep35,
	intelBaselineStep36,
	intelBaselineStep37,
	intelBaselineStep38,
	intelBaselineStep39,
	intelBaselineStep40,
	intelBaselineStep41,
	intelBaselineStep42,
	intelBaselineStep43,
	intelBaselineStep44,
	intelBaselineStep45,
	intelBaselineStep46,
	intelBaselineStep47,
	intelBaselineStep48,
	intelBaselineStep49,
	intelBaselineStep50,
	intelBaselineStep51,
	intelBaselineStep52,
	intelBaselineStep53,
	intelBaselineStep54,
	intelBaselineStep55,
}
