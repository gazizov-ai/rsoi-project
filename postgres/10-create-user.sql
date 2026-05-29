-- file: 10-create-user.sql
CREATE ROLE program WITH PASSWORD 'test';
ALTER ROLE program WITH LOGIN;

CREATE DATABASE payments;
GRANT ALL PRIVILEGES ON DATABASE payments TO program;

CREATE DATABASE reservations;
GRANT ALL PRIVILEGES ON DATABASE reservations TO program;

CREATE DATABASE loyalties;
GRANT ALL PRIVILEGES ON DATABASE loyalties TO program;

CREATE DATABASE identity;
GRANT ALL PRIVILEGES ON DATABASE identity TO program;

CREATE DATABASE statistics;
GRANT ALL PRIVILEGES ON DATABASE statistics TO program;

\connect payments

CREATE TABLE payments
(
    id          SERIAL PRIMARY KEY,
    payment_uid uuid        NOT NULL UNIQUE,
    username    VARCHAR(80) NOT NULL,
    status      VARCHAR(20) NOT NULL
        CHECK (status IN ('PAID', 'CANCELED')),
    price       INT         NOT NULL
);

ALTER TABLE payments OWNER TO program;

\connect loyalties

CREATE TABLE loyalties
(
    id                SERIAL PRIMARY KEY,
    username          VARCHAR(80) NOT NULL UNIQUE,
    reservation_count INT         NOT NULL DEFAULT 0,
    status            VARCHAR(80) NOT NULL DEFAULT 'BRONZE'
        CHECK (status IN ('BRONZE', 'SILVER', 'GOLD')),
    discount          INT         NOT NULL
);

INSERT INTO loyalties (id, username, reservation_count, status, discount)
VALUES (
    1,
    'Test Max',
    25,
    'GOLD',
    10
);


ALTER TABLE loyalties OWNER TO program;

\connect reservations

CREATE TABLE hotels
(
    id        SERIAL PRIMARY KEY,
    hotel_uid uuid         NOT NULL UNIQUE,
    name      VARCHAR(255) NOT NULL,
    country   VARCHAR(80)  NOT NULL,
    city      VARCHAR(80)  NOT NULL,
    address   VARCHAR(255) NOT NULL,
    stars     INT,
    price     INT          NOT NULL
);

INSERT INTO hotels (id, hotel_uid, name, country, city, address, stars, price)
VALUES (
    1,
    '049161bb-badd-4fa8-9d90-87c9a82b0668',
    'Ararat Park Hyatt Moscow',
    'Россия',
    'Москва',
    'Неглинная ул., 4',
    5,
    10000
);

CREATE TABLE reservations
(
    id              SERIAL PRIMARY KEY,
    reservation_uid uuid UNIQUE NOT NULL,
    username        VARCHAR(80) NOT NULL,
    payment_uid     uuid        NOT NULL,
    hotel_id        INT REFERENCES hotels (id),
    status          VARCHAR(20) NOT NULL
        CHECK (status IN ('PAID', 'CANCELED')),
    start_date      TIMESTAMP WITH TIME ZONE,
    end_data        TIMESTAMP WITH TIME ZONE
);

ALTER TABLE hotels OWNER TO program;
ALTER TABLE reservations OWNER TO program;
