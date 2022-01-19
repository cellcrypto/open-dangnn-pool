CREATE TABLE `blocks` (
    `state` TINYINT(4) NULL DEFAULT NULL,
    `coin` VARCHAR(20) NULL DEFAULT '' COLLATE 'utf8mb3_general_ci',
    `round_height` BIGINT(20) NULL DEFAULT NULL,
    `nonce` VARCHAR(100) NULL DEFAULT NULL COLLATE 'utf8mb3_general_ci',
    `height` BIGINT(20) NULL DEFAULT '0',
    `hash_no_nonce` VARCHAR(100) NULL DEFAULT NULL COLLATE 'utf8mb3_general_ci',
    `mix_digest` VARCHAR(100) NULL DEFAULT NULL COLLATE 'utf8mb3_general_ci',
    `round_diff` BIGINT(20) NULL DEFAULT NULL,
    `total_share` BIGINT(20) NULL DEFAULT '0',
    `insert_time` VARCHAR(100) NULL DEFAULT NULL COLLATE 'utf8mb3_general_ci',
    `uncle_height` BIGINT(20) NULL DEFAULT '0',
    `orphan` TINYINT(4) NULL DEFAULT '0',
    `hash` VARCHAR(68) NULL DEFAULT '' COLLATE 'utf8mb3_general_ci',
    `timestamp` BIGINT(20) NULL DEFAULT '0',
    `diff` BIGINT(20) NULL DEFAULT '0',
    `total_diff` BIGINT(20) NULL DEFAULT '0',
    `reward` VARCHAR(32) NULL DEFAULT '0' COLLATE 'utf8mb3_general_ci',
    `total_immatured_cnt` INT(11) NULL DEFAULT '0',
    `total_immatured` BIGINT(20) NULL DEFAULT '0',
    INDEX `nonce_idx` (`state`, `round_height`, `nonce`) USING BTREE,
    INDEX `height_idx` (`state`, `height`) USING BTREE
)
COLLATE='utf8mb3_general_ci'
ENGINE=InnoDB;


CREATE TABLE `credits_balance` (
    `coin` VARCHAR(20) NOT NULL DEFAULT '' COLLATE 'utf8mb3_general_ci',
    `round_height` BIGINT(20) NOT NULL,
    `height` BIGINT(20) NOT NULL,
    `hash` VARCHAR(68) NOT NULL COLLATE 'utf8mb3_general_ci',
    `login_addr` VARCHAR(50) NOT NULL COLLATE 'utf8mb3_general_ci',
    `amount` VARCHAR(30) NULL DEFAULT NULL COLLATE 'utf8mb3_general_ci',
    `percent` DECIMAL(20,9) NULL DEFAULT '0.000000000',
    `timestamp` BIGINT(20) NULL DEFAULT '0',
    `insert_cnt` INT(11) NULL DEFAULT '1',
    `insert_time` TIMESTAMP NULL DEFAULT current_timestamp(),
    PRIMARY KEY (`height`, `hash`, `login_addr`) USING BTREE,
    INDEX `login_idx` (`login_addr`) USING BTREE
)
COLLATE='utf8mb3_general_ci'
ENGINE=InnoDB;


CREATE TABLE `credits_blocks` (
    `height` BIGINT(20) NOT NULL DEFAULT '0',
    `hash` VARCHAR(68) NOT NULL DEFAULT '' COLLATE 'utf8mb3_general_ci',
    `coin` VARCHAR(20) NULL DEFAULT NULL COLLATE 'utf8mb3_general_ci',
    `reward` VARCHAR(32) NULL DEFAULT '0' COLLATE 'utf8mb3_general_ci',
    `timestamp` TIMESTAMP NULL DEFAULT current_timestamp(),
    PRIMARY KEY (`height`, `hash`) USING BTREE,
    INDEX `coin` (`coin`, `height`) USING BTREE
)
COLLATE='utf8mb3_general_ci'
ENGINE=InnoDB;


CREATE TABLE `credits_immature` (
    `coin` VARCHAR(20) NULL DEFAULT NULL COLLATE 'utf8mb3_general_ci',
    `round_height` BIGINT(20) NOT NULL,
    `height` BIGINT(20) NOT NULL,
    `hash` VARCHAR(68) NOT NULL COLLATE 'utf8mb3_general_ci',
    `login_addr` VARCHAR(50) NOT NULL COLLATE 'utf8mb3_general_ci',
    `amount` VARCHAR(30) NULL DEFAULT NULL COLLATE 'utf8mb3_general_ci',
    `percent` DECIMAL(20,9) NULL DEFAULT NULL,
    `timestamp` BIGINT(20) NULL DEFAULT NULL,
    PRIMARY KEY (`round_height`, `hash`, `login_addr`) USING BTREE
)
COLLATE='utf8mb3_general_ci'
ENGINE=InnoDB;


CREATE TABLE `finances` (
    `coin` VARCHAR(20) NOT NULL DEFAULT '' COLLATE 'utf8mb3_general_ci',
    `immature` BIGINT(20) NULL DEFAULT '0',
    `pending` BIGINT(20) NULL DEFAULT '0',
    `balance` BIGINT(20) NULL DEFAULT '0',
    `paid` BIGINT(20) NULL DEFAULT '0',
    `last_height` BIGINT(20) NULL DEFAULT '0',
    `last_hash` VARCHAR(68) NULL DEFAULT NULL COLLATE 'utf8mb3_general_ci',
    `total_mined` BIGINT(20) NULL DEFAULT '0',
    `payout_cnt` BIGINT(20) NULL DEFAULT '0',
    PRIMARY KEY (`coin`) USING BTREE
)
COLLATE='utf8mb3_general_ci'
ENGINE=InnoDB;


CREATE TABLE `miner_charts` (
    `login_addr` VARCHAR(68) NOT NULL DEFAULT '' COLLATE 'utf8mb3_general_ci',
    `time` BIGINT(20) NOT NULL DEFAULT '0',
    `time2` TIMESTAMP NULL DEFAULT NULL,
    `hash` BIGINT(20) NULL DEFAULT '0',
    `large_hash` BIGINT(20) NULL DEFAULT '0',
    `report_hash` BIGINT(20) NULL DEFAULT '0',
    `share` INT(11) NULL DEFAULT NULL,
    `work_online` INT(11) NULL DEFAULT NULL,
    PRIMARY KEY (`login_addr`, `time`) USING BTREE
)
COLLATE='utf8mb3_general_ci'
ENGINE=InnoDB;


CREATE TABLE `miner_info` (
    `coin` VARCHAR(20) NOT NULL COLLATE 'utf8mb3_general_ci',
    `login_addr` VARCHAR(50) NOT NULL COLLATE 'utf8mb3_general_ci',
    `balance` BIGINT(20) NULL DEFAULT '0',
    `pending` BIGINT(20) NULL DEFAULT '0',
    `paid` BIGINT(20) NULL DEFAULT '0',
    `blocks_found` INT(11) NULL DEFAULT '0',
    `immature` BIGINT(20) NULL DEFAULT '0',
    `matured` BIGINT(20) NULL DEFAULT '0',
    `share` INT(11) NULL DEFAULT '0',
    `share_check` BIGINT(20) NULL DEFAULT '0',
    `last_share` TIMESTAMP NULL DEFAULT current_timestamp(),
    `id` VARCHAR(50) NULL DEFAULT '' COLLATE 'utf8mb3_general_ci',
    `diff_times` INT(11) NULL DEFAULT '0',
    `payout_lock` BIGINT(20) NULL DEFAULT '0',
    `payout_limit` BIGINT(20) NULL DEFAULT '0',
    `payout_cnt` BIGINT(20) NULL DEFAULT '0',
    `payout_last` TIMESTAMP NULL DEFAULT NULL,
    `hostname` VARCHAR(50) NULL DEFAULT '' COLLATE 'utf8mb3_general_ci',
    `insert_time` TIMESTAMP NULL DEFAULT current_timestamp(),
    PRIMARY KEY (`coin`, `login_addr`) USING BTREE,
    INDEX `time_idx` (`insert_time`) USING BTREE,
    INDEX `balance_idx` (`balance`, `payout_limit`) USING BTREE
)
COLLATE='utf8mb3_general_ci'
ENGINE=InnoDB;


CREATE TABLE `payments_all` (
    `seq` BIGINT(20) NOT NULL AUTO_INCREMENT,
    `login_addr` VARCHAR(68) NOT NULL DEFAULT '0x0' COLLATE 'utf8mb3_general_ci',
    `from` VARCHAR(68) NOT NULL DEFAULT '0x0' COLLATE 'utf8mb3_general_ci',
    `tx_hash` VARCHAR(128) NULL DEFAULT NULL COLLATE 'utf8mb3_general_ci',
    `amount` BIGINT(20) NULL DEFAULT NULL,
    `coin` VARCHAR(20) NULL DEFAULT '' COLLATE 'utf8mb3_general_ci',
    `insert_time` TIMESTAMP NULL DEFAULT current_timestamp(),
    PRIMARY KEY (`seq`) USING BTREE,
    INDEX `login_addr` (`login_addr`) USING BTREE
)
COLLATE='utf8mb3_general_ci'
ENGINE=InnoDB
AUTO_INCREMENT=9;


CREATE TABLE `log` (
    `id` BIGINT(20) UNSIGNED NOT NULL AUTO_INCREMENT,
    `msg_type` INT(10) UNSIGNED NOT NULL DEFAULT '0',
    `msg_err` INT(11) NULL DEFAULT NULL,
    `where` VARCHAR(20) NOT NULL DEFAULT '' COLLATE 'utf8mb3_general_ci',
    `round_height` BIGINT(20) NULL DEFAULT '0',
    `height` BIGINT(20) NULL DEFAULT '0',
    `addr` VARCHAR(50) NOT NULL COLLATE 'utf8mb3_general_ci',
    `addr2` VARCHAR(50) NULL DEFAULT NULL COLLATE 'utf8mb3_general_ci',
    `msg` VARCHAR(700) NULL DEFAULT '' COLLATE 'utf8mb3_general_ci',
    `insert_time` TIMESTAMP NOT NULL DEFAULT current_timestamp(),
    PRIMARY KEY (`id`) USING BTREE,
    INDEX `time_idx` (`insert_time`) USING BTREE
)
COLLATE='utf8mb3_general_ci'
ENGINE=InnoDB
AUTO_INCREMENT=1;


CREATE TABLE `miner_sub` (
    `login_addr` VARCHAR(68) NOT NULL DEFAULT '' COLLATE 'utf8mb3_general_ci',
    `sub_addr` VARCHAR(68) NOT NULL DEFAULT '' COLLATE 'utf8mb3_general_ci',
    `weight` INT(11) NULL DEFAULT NULL,
    PRIMARY KEY (`login_addr`, `sub_addr`) USING BTREE
)
COLLATE='utf8mb3_general_ci'
ENGINE=InnoDB;


CREATE TABLE `inbound_id` (
    `coin` VARCHAR(20) NULL DEFAULT NULL COLLATE 'utf8mb3_general_ci',
    `id` VARCHAR(68) NULL DEFAULT NULL COLLATE 'utf8mb3_general_ci',
    `rule` VARCHAR(20) NULL DEFAULT NULL COLLATE 'utf8mb3_general_ci',
    `desc` VARBINARY(300) NULL DEFAULT NULL
)
COLLATE='utf8mb3_general_ci'
ENGINE=InnoDB;


CREATE TABLE `inbound_ip` (
    `coin` VARCHAR(20) NULL DEFAULT NULL COLLATE 'utf8mb3_general_ci',
    `ip` VARCHAR(50) NULL DEFAULT NULL COLLATE 'utf8mb3_general_ci',
    `rule` VARCHAR(20) NULL DEFAULT NULL COLLATE 'utf8mb3_general_ci',
    `desc` VARBINARY(300) NULL DEFAULT NULL
)
COLLATE='utf8mb3_general_ci'
ENGINE=InnoDB;

CREATE TABLE `ban_whitelist` (
    `coin` VARCHAR(20) NULL DEFAULT NULL COLLATE 'utf8mb3_general_ci',
    `ip_addr` VARCHAR(50) NULL DEFAULT NULL COLLATE 'utf8mb3_general_ci'
)
COLLATE='utf8mb3_general_ci'
ENGINE=InnoDB;
