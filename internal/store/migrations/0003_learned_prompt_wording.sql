-- The progress check interval is configurable, so the 1:1 prompt no longer
-- names a number of weeks. Prompts the owner already edited are left alone.
UPDATE meeting_prompt
SET prompt_text = 'What did you learn since the last progress check?'
WHERE meeting_kind = 'one_on_one'
  AND prompt_key = 'learned'
  AND prompt_text = 'What did you learn in the last 2 weeks?';
