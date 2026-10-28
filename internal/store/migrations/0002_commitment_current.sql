-- The current status of a commitment is its latest status event.
CREATE VIEW commitment_current AS
SELECT
    c.*,
    COALESCE((SELECT ev.status FROM commitment_event ev WHERE ev.commitment_id = c.id ORDER BY ev.id DESC LIMIT 1), 'open') AS status,
    COALESCE((SELECT ev.note FROM commitment_event ev WHERE ev.commitment_id = c.id ORDER BY ev.id DESC LIMIT 1), '') AS status_note
FROM commitment c;
