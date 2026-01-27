CREATE DATABASE IF NOT EXISTS linkgo_im;
USE linkgo_im;

CREATE TABLE IF NOT EXISTS chat_histories (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id VARCHAR(64) NOT NULL COMMENT '消息属于谁',
    seq BIGINT NOT NULL COMMENT '消息序列号',
    msg_id VARCHAR(64) NOT NULL COMMENT '消息全局唯一ID',
    group_id VARCHAR(64) NOT NULL DEFAULT '' COMMENT '群ID',
    cmd INT NOT NULL COMMENT '消息类型',
    from_user_id VARCHAR(64) NOT NULL COMMENT '发送者',
    to_user_id VARCHAR(64) NOT NULL COMMENT '接收者',
    content BLOB NOT NULL COMMENT '消息内容(Protobuf)',
    create_time BIGINT NOT NULL COMMENT '发送时间',
    INDEX idx_user_seq (user_id, seq)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;