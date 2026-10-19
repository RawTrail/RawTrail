-- +goose Up
-- Re-credit alerts raised by this project's own rules from "Fleet EDR" to "RawTrail", with the product rename (#1366). The credit
-- is a snapshot taken when the alert was raised (00012), so without this every existing alert would keep naming the old product
-- beside every new one naming the new. Upstream credits ("SigmaHQ, by ...") and blank pre-attribution rows are untouched. updated_at
-- keeps its value, as in the origin backfill, because re-crediting is not triage activity and the alert prune reads it.
--
-- A data rewrite, not a schema change, so ADR-0009's expand and contract does not apply. A replica still on the previous release
-- during a rolling upgrade can raise a few more alerts with the old credit before it restarts; those keep it.

-- +goose StatementBegin
UPDATE alerts SET origin = 'RawTrail', updated_at = updated_at WHERE origin = 'Fleet EDR';
-- +goose StatementEnd

-- +goose Down
-- Forward-only migrations (ADR-0009).
