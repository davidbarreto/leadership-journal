-- The 1:1 itself is now what is tracked, not a separate progress check, so the
-- prompt refers to the last 1:1. Prompts the owner already edited are left alone.
UPDATE meeting_prompt
SET prompt_text = 'What did you learn since our last 1:1?'
WHERE meeting_kind = 'one_on_one'
  AND prompt_key = 'learned'
  AND prompt_text = 'What did you learn since the last progress check?';
