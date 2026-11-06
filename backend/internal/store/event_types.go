package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type EventType struct {
	Name           string    `json:"name"`
	Title          string    `json:"title"`
	Template       string    `json:"template"`
	Icon           string    `json:"icon"`
	Category       string    `json:"category"`
	Color          string    `json:"color"`
	FeaturedFields []string  `json:"featured_fields"`
	Discovered     bool      `json:"discovered"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type FieldRule struct {
	Field  string `json:"field"`
	Label  string `json:"label"`
	Hidden bool   `json:"hidden"`
	Format string `json:"format"`
	Redact bool   `json:"redact"`
}

func (s *Store) ListEventTypes(ctx context.Context, project uuid.UUID) ([]EventType, error) {
	rows, err := s.pool.Query(ctx, `SELECT name,title,template,icon,category,color,featured_fields,discovered,created_at,updated_at FROM event_types WHERE project_id=$1 ORDER BY name LIMIT 5000`, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EventType{}
	for rows.Next() {
		var e EventType
		if err := rows.Scan(&e.Name, &e.Title, &e.Template, &e.Icon, &e.Category, &e.Color, &e.FeaturedFields, &e.Discovered, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) GetEventType(ctx context.Context, project uuid.UUID, name string) (EventType, error) {
	var e EventType
	err := s.pool.QueryRow(ctx, `SELECT name,title,template,icon,category,color,featured_fields,discovered,created_at,updated_at FROM event_types WHERE project_id=$1 AND name=$2`, project, name).Scan(&e.Name, &e.Title, &e.Template, &e.Icon, &e.Category, &e.Color, &e.FeaturedFields, &e.Discovered, &e.CreatedAt, &e.UpdatedAt)
	return e, mapErr(err)
}

func (s *Store) SaveEventType(ctx context.Context, project uuid.UUID, e EventType) (EventType, error) {
	if e.FeaturedFields == nil {
		e.FeaturedFields = []string{}
	}
	err := s.pool.QueryRow(ctx, `INSERT INTO event_types(project_id,name,title,template,icon,category,color,featured_fields) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(project_id,name) DO UPDATE SET title=EXCLUDED.title,template=EXCLUDED.template,icon=EXCLUDED.icon,category=EXCLUDED.category,color=EXCLUDED.color,featured_fields=EXCLUDED.featured_fields,discovered=false,updated_at=now() RETURNING created_at,updated_at`, project, e.Name, e.Title, e.Template, e.Icon, e.Category, e.Color, e.FeaturedFields).Scan(&e.CreatedAt, &e.UpdatedAt)
	return e, mapErr(err)
}

func (s *Store) DeleteEventType(ctx context.Context, project uuid.UUID, name string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM event_types WHERE project_id=$1 AND name=$2`, project, name)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DiscoverEventTypes(ctx context.Context, project uuid.UUID, names []string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO event_types(project_id,name,discovered) SELECT $1,n,true FROM unnest($2::text[]) n WHERE n<>'' ON CONFLICT DO NOTHING`, project, names)
	return err
}

func (s *Store) FieldRules(ctx context.Context, project uuid.UUID) ([]FieldRule, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT COALESCE((SELECT rules FROM project_field_rules WHERE project_id=$1),'[]'::jsonb)`, project).Scan(&raw)
	if err != nil {
		return nil, err
	}
	var out []FieldRule
	err = json.Unmarshal(raw, &out)
	return out, err
}

func (s *Store) SaveFieldRules(ctx context.Context, project uuid.UUID, rules []FieldRule) error {
	raw, err := json.Marshal(rules)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO project_field_rules(project_id,rules) VALUES($1,$2) ON CONFLICT(project_id) DO UPDATE SET rules=EXCLUDED.rules`, project, raw)
	return err
}

func (s *Store) ApplyEventPreset(ctx context.Context, project uuid.UUID, events []EventType) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		for _, e := range events {
			_, err := tx.Exec(ctx, `INSERT INTO event_types(project_id,name,title,template,icon,category,color,featured_fields) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(project_id,name) DO UPDATE SET title=EXCLUDED.title,template=EXCLUDED.template,icon=EXCLUDED.icon,category=EXCLUDED.category,color=EXCLUDED.color,featured_fields=EXCLUDED.featured_fields,discovered=false,updated_at=now() WHERE event_types.discovered`, project, e.Name, e.Title, e.Template, e.Icon, e.Category, e.Color, e.FeaturedFields)
			if err != nil {
				return mapErr(err)
			}
		}
		return nil
	})
}
