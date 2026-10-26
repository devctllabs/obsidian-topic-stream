DROP TABLE item_summary_retries;
DROP TABLE digest_item_summaries;
ALTER TABLE item_summary_inputs DROP CONSTRAINT item_summary_inputs_ready_fk;
DROP TABLE item_summaries;
DROP TABLE item_summary_inputs;
