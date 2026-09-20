DROP TABLE IF EXISTS feeder_lines;
DROP TYPE IF EXISTS ampacity_source;
DROP TRIGGER IF EXISTS feeder_nodes_no_cycle ON feeder_nodes;
DROP TABLE IF EXISTS feeder_nodes;
DROP FUNCTION IF EXISTS reject_feeder_node_cycle();
DROP TABLE IF EXISTS feeders;
