package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	services "github.com/elum2b/services"
)

func (r *Repository) Export(
	ctx context.Context,
	workspaceID string,
	req ExportRequest,
) (ExportPackage, error) {
	if err := services.ValidateWorkspaceID(workspaceID); err != nil {
		return ExportPackage{}, err
	}

	now := req.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}

	var (
		bundles        []OfferBundle
		outCodes       []ExportCode
		outAssignments []ExportAssignment
	)

	if err := r.WithReadOnlySnapshot(ctx, func(txRepo *Repository) error {
		var err error

		bundles, err = txRepo.ListAllOfferBundles(ctx, workspaceID)
		if err != nil {
			return err
		}

		outCodes, outAssignments, err = txRepo.exportRuntime(ctx, workspaceID)

		return err
	}); err != nil {
		return ExportPackage{}, err
	}

	out := ExportPackage{
		Format:    ExportFormat,
		Service:   "cpa",
		CreatedAt: now.UTC(),
		Offers:    make([]ExportOffer, 0, len(bundles)),
	}

	out.Codes = outCodes
	out.Assignments = outAssignments

	for _, bundle := range bundles {
		offer := ExportOffer{
			ID:                bundle.Offer.ID,
			Payload:           bundle.Offer.Payload,
			Target:            nullableJSON(bundle.Offer.Target),
			CodeMode:          bundle.Offer.CodeMode,
			CodeSource:        bundle.Offer.CodeSource,
			SharedCode:        bundle.Offer.SharedCode,
			GeneratedLength:   bundle.Offer.GeneratedLength,
			GeneratedAlphabet: bundle.Offer.GeneratedAlphabet,
			IsActive:          bundle.Offer.IsActive,
			StartAt:           bundle.Offer.StartAt,
			EndAt:             bundle.Offer.EndAt,
			Localization: make(
				map[string]ExportText,
				len(bundle.Localizations),
			),
			Rewards: make([]ExportReward, 0, len(bundle.Rewards)),
		}
		for _, localization := range bundle.Localizations {
			offer.Localization[localization.Locale] = ExportText{
				Title:       localization.Title,
				Description: localization.Description,
			}
		}

		for _, reward := range bundle.Rewards {
			offer.Rewards = append(offer.Rewards, ExportReward{
				Key:      reward.Key,
				Type:     reward.Type,
				Quantity: reward.Quantity,
				Scale:    reward.Scale,
				Unit:     reward.Unit,
			})
		}

		out.Offers = append(out.Offers, offer)
	}

	return out, nil
}

func (r *Repository) exportRuntime(
	ctx context.Context,
	workspaceID string,
) ([]ExportCode, []ExportAssignment, error) {
	codes := []ExportCode{}

	rows, err := r.executor.QueryContext(
		ctx,
		`SELECT cpa_id, code, source::text, status::text, created_at, updated_at, deleted_at FROM cpa_code WHERE workspace_id = $1 ORDER BY id`,
		workspaceID,
	)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			v       ExportCode
			deleted sql.NullTime
		)

		if err := rows.Scan(
			&v.CPAID,
			&v.Code,
			&v.Source,
			&v.Status,
			&v.CreatedAt,
			&v.UpdatedAt,
			&deleted,
		); err != nil {
			return nil, nil, err
		}

		if deleted.Valid {
			value := deleted.Time

			v.DeletedAt = &value
		}

		codes = append(codes, v)
	}

	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	assignments := []ExportAssignment{}

	rows, err = r.executor.QueryContext(
		ctx,
		`SELECT a.cpa_id, a.app_id, a.platform_id, a.platform_user_id, a.code, a.code_mode::text, c.code, a.rewards_snapshot, a.status::text, a.issued_at, a.completed_at, e.event_type::text, e.occurred_at FROM cpa_assignment a LEFT JOIN cpa_code c ON c.id = a.code_id LEFT JOIN cpa_assignment_event e ON e.assignment_id = a.id WHERE a.workspace_id = $1 ORDER BY a.id, e.occurred_at`,
		workspaceID,
	)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var previous string

	for rows.Next() {
		var (
			v         ExportAssignment
			ref       sql.NullString
			completed sql.NullTime
			eventType sql.NullString
			eventAt   sql.NullTime
		)

		if err := rows.Scan(
			&v.CPAID,
			&v.AppID,
			&v.PlatformID,
			&v.PlatformUserID,
			&v.Code,
			&v.CodeMode,
			&ref,
			&v.RewardsSnapshot,
			&v.Status,
			&v.IssuedAt,
			&completed,
			&eventType,
			&eventAt,
		); err != nil {
			return nil, nil, err
		}

		if ref.Valid {
			value := ref.String

			v.CodeRef = &value
		}

		if completed.Valid {
			value := completed.Time

			v.CompletedAt = &value
		}

		key := fmt.Sprintf(
			"%s\x00%d\x00%d\x00%s",
			v.CPAID,
			v.AppID,
			v.PlatformID,
			v.PlatformUserID,
		)
		if key != previous {
			assignments = append(assignments, v)
			previous = key
		}

		if eventType.Valid {
			assignments[len(assignments)-1].Events = append(
				assignments[len(assignments)-1].Events,
				ExportAssignmentEvent{
					EventType:  eventType.String,
					OccurredAt: eventAt.Time,
				},
			)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	return codes, assignments, nil
}

func nullableJSON(value []byte) []byte {
	if len(value) == 0 || string(value) == "null" {
		return nil
	}

	return value
}
