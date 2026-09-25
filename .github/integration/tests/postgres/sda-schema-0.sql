-- Migration test fixture: an sda database at schema version 0
-- (local_ega.dbschema_version, applied 2023-09-20 09:54:13.647297+00).
--
-- Produced on 2026-09-24 from the former binary fixture pgdata.tgz (a
-- PostgreSQL 15 data directory): started postgres:15-alpine on it, then
--   pg_dump -d sda > sda.sql       (pg_dump 15.19)
-- and prepended the two non-superuser roles from pg_dumpall --roles-only.
-- The seed step in .github/integration/postgres.yml restores this file
-- into a freshly initialised cluster before the migrate service runs the
-- scripts in postgresql/migratedb.d. Plain SQL survives major bumps.

CREATE ROLE lega_in WITH NOSUPERUSER INHERIT NOCREATEROLE NOCREATEDB LOGIN NOREPLICATION NOBYPASSRLS;
CREATE ROLE lega_out WITH NOSUPERUSER INHERIT NOCREATEROLE NOCREATEDB LOGIN NOREPLICATION NOBYPASSRLS;

--
-- PostgreSQL database dump
--

\restrict kPohhoV0ak2tqryoKWXZcssxFfxjWhlVXmKFZTo1yb1HCuc3vfI2bA7nViJjO3Y

-- Dumped from database version 15.19
-- Dumped by pg_dump version 15.19

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

--
-- Name: local_ega; Type: SCHEMA; Schema: -; Owner: postgres
--

CREATE SCHEMA local_ega;


ALTER SCHEMA local_ega OWNER TO postgres;

--
-- Name: local_ega_download; Type: SCHEMA; Schema: -; Owner: postgres
--

CREATE SCHEMA local_ega_download;


ALTER SCHEMA local_ega_download OWNER TO postgres;

--
-- Name: local_ega_ebi; Type: SCHEMA; Schema: -; Owner: postgres
--

CREATE SCHEMA local_ega_ebi;


ALTER SCHEMA local_ega_ebi OWNER TO postgres;

--
-- Name: checksum_algorithm; Type: TYPE; Schema: local_ega; Owner: postgres
--

CREATE TYPE local_ega.checksum_algorithm AS ENUM (
    'MD5',
    'SHA256',
    'SHA384',
    'SHA512'
);


ALTER TYPE local_ega.checksum_algorithm OWNER TO postgres;

--
-- Name: storage; Type: TYPE; Schema: local_ega; Owner: postgres
--

CREATE TYPE local_ega.storage AS ENUM (
    'S3',
    'POSIX'
);


ALTER TYPE local_ega.storage OWNER TO postgres;

--
-- Name: request_type; Type: TYPE; Schema: local_ega_download; Owner: postgres
--

CREATE TYPE local_ega_download.request_type AS (
	req_id integer,
	header text,
	archive_path text,
	archive_type local_ega.storage,
	file_size integer,
	archive_file_checksum character varying,
	archive_file_checksum_type local_ega.checksum_algorithm
);


ALTER TYPE local_ega_download.request_type OWNER TO postgres;

--
-- Name: check_session_keys_checksums_sha256(text[]); Type: FUNCTION; Schema: local_ega; Owner: postgres
--

CREATE FUNCTION local_ega.check_session_keys_checksums_sha256(checksums text[]) RETURNS boolean
    LANGUAGE plpgsql
    AS $$
    #variable_conflict use_column
    BEGIN
	RETURN EXISTS(SELECT 1
                      FROM local_ega.session_key_checksums_sha256 sk 
	              INNER JOIN local_ega.files f
		      ON f.id = sk.file_id 
		      WHERE (f.status <> 'ERROR' AND f.status <> 'DISABLED') AND -- no data-race on those values
		      	    sk.session_key_checksum = ANY(checksums));
    END;
$$;


ALTER FUNCTION local_ega.check_session_keys_checksums_sha256(checksums text[]) OWNER TO postgres;

--
-- Name: finalize_file(text, text, character varying, character varying, text); Type: FUNCTION; Schema: local_ega; Owner: postgres
--

CREATE FUNCTION local_ega.finalize_file(inpath text, eid text, checksum character varying, checksum_type character varying, sid text) RETURNS void
    LANGUAGE plpgsql
    AS $$
    #variable_conflict use_column
    BEGIN
	-- -- Check if in proper state
	-- IF EXISTS(SELECT id
	--    	  FROM local_ega.main
	-- 	  WHERE archive_file_checksum = checksum AND
	-- 	  	archive_file_checksum_type = upper(checksum_type)::local_ega.checksum_algorithm AND
	-- 		elixir_id = eid AND
	-- 		inbox_path = inpath AND
	-- 		status <> 'COMPLETED')
	-- THEN
	--    RAISE EXCEPTION 'Archive file not in completed state for stable_id: % ', sid;
	-- END IF;
	-- Go ahead and mark _them_ done
	UPDATE local_ega.files
	SET status = 'READY',
	    stable_id = sid
	WHERE archive_file_checksum = checksum AND
	      archive_file_checksum_type = upper(checksum_type)::local_ega.checksum_algorithm AND
	      elixir_id = eid AND
	      inbox_path = inpath AND
	      status = 'COMPLETED';
    END;
$$;


ALTER FUNCTION local_ega.finalize_file(inpath text, eid text, checksum character varying, checksum_type character varying, sid text) OWNER TO postgres;

--
-- Name: insert_error(integer, text, text, text, boolean); Type: FUNCTION; Schema: local_ega; Owner: postgres
--

CREATE FUNCTION local_ega.insert_error(fid integer, h text, etype text, msg text, from_user boolean) RETURNS void
    LANGUAGE plpgsql
    AS $$
    BEGIN
       INSERT INTO local_ega.errors (file_id,hostname,error_type,msg,from_user) VALUES(fid,h,etype,msg,from_user);
       UPDATE local_ega.files SET status = 'ERROR' WHERE id = fid;
    END;
$$;


ALTER FUNCTION local_ega.insert_error(fid integer, h text, etype text, msg text, from_user boolean) OWNER TO postgres;

--
-- Name: insert_file(text, text); Type: FUNCTION; Schema: local_ega; Owner: postgres
--

CREATE FUNCTION local_ega.insert_file(inpath text, eid text) RETURNS integer
    LANGUAGE plpgsql
    AS $_$
    #variable_conflict use_column
    DECLARE
        file_id  local_ega.main.id%TYPE;
        file_ext local_ega.main.submission_file_extension%TYPE;
    BEGIN
        -- Make a new insertion
        file_ext := substring(inpath from '\.([^\.]*)$'); -- extract extension from filename
	INSERT INTO local_ega.main (submission_file_path,
	  	                    submission_user,
			   	    submission_file_extension,
			  	    status,
			  	    encryption_method) -- hard-code the archive_encryption
	VALUES(inpath,eid,file_ext,'INIT','CRYPT4GH') RETURNING local_ega.main.id
	INTO file_id;
	RETURN file_id;
    END;
$_$;


ALTER FUNCTION local_ega.insert_file(inpath text, eid text) OWNER TO postgres;

--
-- Name: is_disabled(integer); Type: FUNCTION; Schema: local_ega; Owner: postgres
--

CREATE FUNCTION local_ega.is_disabled(fid integer) RETURNS boolean
    LANGUAGE plpgsql
    AS $$
#variable_conflict use_column
BEGIN
   RETURN EXISTS(SELECT 1 FROM local_ega.files WHERE id = fid AND status = 'DISABLED');
END;
$$;


ALTER FUNCTION local_ega.is_disabled(fid integer) OWNER TO postgres;

--
-- Name: main_updated(); Type: FUNCTION; Schema: local_ega; Owner: postgres
--

CREATE FUNCTION local_ega.main_updated() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
     NEW.last_modified = clock_timestamp();
		 RETURN NEW;
END;
$$;


ALTER FUNCTION local_ega.main_updated() OWNER TO postgres;

--
-- Name: mark_ready(); Type: FUNCTION; Schema: local_ega; Owner: postgres
--

CREATE FUNCTION local_ega.mark_ready() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
     UPDATE local_ega.main_errors SET active = FALSE WHERE file_id = NEW.id;  -- or OLD.id
     RETURN NEW;
END;
$$;


ALTER FUNCTION local_ega.mark_ready() OWNER TO postgres;

--
-- Name: download_complete(integer, bigint, double precision); Type: FUNCTION; Schema: local_ega_download; Owner: postgres
--

CREATE FUNCTION local_ega_download.download_complete(rid integer, dlsize bigint, s double precision) RETURNS void
    LANGUAGE plpgsql
    AS $$
BEGIN
     INSERT INTO local_ega_download.success(req_id,bytes,speed)
     VALUES(rid,dlsize,s);
END;
$$;


ALTER FUNCTION local_ega_download.download_complete(rid integer, dlsize bigint, s double precision) OWNER TO postgres;

--
-- Name: insert_error(integer, text, text, text); Type: FUNCTION; Schema: local_ega_download; Owner: postgres
--

CREATE FUNCTION local_ega_download.insert_error(rid integer, h text, etype text, msg text) RETURNS void
    LANGUAGE plpgsql
    AS $$
BEGIN
     INSERT INTO local_ega_download.errors (req_id,hostname,code,description)
     VALUES (rid, h, etype, msg);
END;
$$;


ALTER FUNCTION local_ega_download.insert_error(rid integer, h text, etype text, msg text) OWNER TO postgres;

--
-- Name: make_request(text, text, text, bigint, bigint); Type: FUNCTION; Schema: local_ega_download; Owner: postgres
--

CREATE FUNCTION local_ega_download.make_request(sid text, uinfo text, cip text, scoord bigint DEFAULT 0, ecoord bigint DEFAULT NULL::bigint) RETURNS local_ega_download.request_type
    LANGUAGE plpgsql
    AS $$
#variable_conflict use_column
DECLARE
     req  local_ega_download.request_type;
     archive_rec local_ega.archive_files%ROWTYPE;
     rid  INTEGER;
BEGIN

     -- Find the file
     SELECT * INTO archive_rec FROM local_ega.archive_files WHERE stable_id = sid LIMIT 1;

     IF archive_rec IS NULL THEN
     	RAISE EXCEPTION 'archived file not found for stable_id: % ', sid;
     END IF;

     -- New entry, or reuse old entry
     INSERT INTO local_ega_download.requests (file_id, user_info, client_ip, start_coordinate, end_coordinate)
     VALUES (archive_rec.file_id, uinfo, cip, scoord, ecoord)
     ON CONFLICT (id) DO NOTHING
     RETURNING local_ega_download.requests.id INTO rid;
     
     -- result
     req.req_id                    := rid;
     req.header                    := archive_rec.header;
     req.archive_path              := archive_rec.archive_path;
     req.archive_type              := archive_rec.archive_type;
     req.file_size                 := archive_rec.archive_filesize;
     req.archive_file_checksum      := archive_rec.archive_file_checksum;
     req.archive_file_checksum_type := archive_rec.archive_file_checksum_type;
     RETURN req;
END;
$$;


ALTER FUNCTION local_ega_download.make_request(sid text, uinfo text, cip text, scoord bigint, ecoord bigint) OWNER TO postgres;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: archive_encryption; Type: TABLE; Schema: local_ega; Owner: postgres
--

CREATE TABLE local_ega.archive_encryption (
    mode character varying(16) NOT NULL,
    description text
);


ALTER TABLE local_ega.archive_encryption OWNER TO postgres;

--
-- Name: main; Type: TABLE; Schema: local_ega; Owner: postgres
--

CREATE TABLE local_ega.main (
    id integer NOT NULL,
    stable_id text,
    status character varying NOT NULL,
    submission_file_path text NOT NULL,
    submission_file_extension character varying(260) NOT NULL,
    submission_file_calculated_checksum character varying(128),
    submission_file_calculated_checksum_type local_ega.checksum_algorithm,
    submission_file_size bigint,
    submission_user text NOT NULL,
    archive_file_reference text,
    archive_file_type local_ega.storage,
    archive_file_size bigint,
    archive_file_checksum character varying(128),
    archive_file_checksum_type local_ega.checksum_algorithm,
    encryption_method character varying,
    version integer,
    header text,
    created_by name DEFAULT CURRENT_USER,
    last_modified_by name DEFAULT CURRENT_USER,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    last_modified timestamp with time zone DEFAULT clock_timestamp() NOT NULL
);


ALTER TABLE local_ega.main OWNER TO postgres;

--
-- Name: archive_files; Type: VIEW; Schema: local_ega; Owner: postgres
--

CREATE VIEW local_ega.archive_files AS
 SELECT main.id AS file_id,
    main.stable_id,
    main.archive_file_reference AS archive_path,
    main.archive_file_type AS archive_type,
    main.archive_file_size AS archive_filesize,
    main.archive_file_checksum,
    main.archive_file_checksum_type,
    main.header,
    main.version
   FROM local_ega.main
  WHERE ((main.status)::text = 'READY'::text);


ALTER TABLE local_ega.archive_files OWNER TO postgres;

--
-- Name: dbschema_version; Type: TABLE; Schema: local_ega; Owner: postgres
--

CREATE TABLE local_ega.dbschema_version (
    version integer NOT NULL,
    applied timestamp with time zone,
    description character varying(1024)
);


ALTER TABLE local_ega.dbschema_version OWNER TO postgres;

--
-- Name: main_errors; Type: TABLE; Schema: local_ega; Owner: postgres
--

CREATE TABLE local_ega.main_errors (
    id integer NOT NULL,
    active boolean DEFAULT true NOT NULL,
    file_id integer NOT NULL,
    hostname text,
    error_type text NOT NULL,
    msg text NOT NULL,
    from_user boolean DEFAULT false,
    occured_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL
);


ALTER TABLE local_ega.main_errors OWNER TO postgres;

--
-- Name: errors; Type: VIEW; Schema: local_ega; Owner: postgres
--

CREATE VIEW local_ega.errors AS
 SELECT main_errors.id,
    main_errors.file_id,
    main_errors.hostname,
    main_errors.error_type,
    main_errors.msg,
    main_errors.from_user,
    main_errors.occured_at
   FROM local_ega.main_errors
  WHERE (main_errors.active = true);


ALTER TABLE local_ega.errors OWNER TO postgres;

--
-- Name: files; Type: VIEW; Schema: local_ega; Owner: postgres
--

CREATE VIEW local_ega.files AS
 SELECT main.id,
    main.submission_user AS elixir_id,
    main.submission_file_path AS inbox_path,
    main.submission_file_size AS inbox_filesize,
    main.submission_file_calculated_checksum AS inbox_file_checksum,
    main.submission_file_calculated_checksum_type AS inbox_file_checksum_type,
    main.status,
    main.archive_file_reference AS archive_path,
    main.archive_file_type AS archive_type,
    main.archive_file_size AS archive_filesize,
    main.archive_file_checksum,
    main.archive_file_checksum_type,
    main.stable_id,
    main.header,
    main.version,
    main.created_at,
    main.last_modified
   FROM local_ega.main;


ALTER TABLE local_ega.files OWNER TO postgres;

--
-- Name: main_errors_id_seq; Type: SEQUENCE; Schema: local_ega; Owner: postgres
--

CREATE SEQUENCE local_ega.main_errors_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER TABLE local_ega.main_errors_id_seq OWNER TO postgres;

--
-- Name: main_errors_id_seq; Type: SEQUENCE OWNED BY; Schema: local_ega; Owner: postgres
--

ALTER SEQUENCE local_ega.main_errors_id_seq OWNED BY local_ega.main_errors.id;


--
-- Name: main_id_seq; Type: SEQUENCE; Schema: local_ega; Owner: postgres
--

CREATE SEQUENCE local_ega.main_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER TABLE local_ega.main_id_seq OWNER TO postgres;

--
-- Name: main_id_seq; Type: SEQUENCE OWNED BY; Schema: local_ega; Owner: postgres
--

ALTER SEQUENCE local_ega.main_id_seq OWNED BY local_ega.main.id;


--
-- Name: session_key_checksums_sha256; Type: TABLE; Schema: local_ega; Owner: postgres
--

CREATE TABLE local_ega.session_key_checksums_sha256 (
    session_key_checksum character varying(128) NOT NULL,
    session_key_checksum_type local_ega.checksum_algorithm,
    file_id integer NOT NULL
);


ALTER TABLE local_ega.session_key_checksums_sha256 OWNER TO postgres;

--
-- Name: status; Type: TABLE; Schema: local_ega; Owner: postgres
--

CREATE TABLE local_ega.status (
    id integer NOT NULL,
    code character varying(16) NOT NULL,
    description text
);


ALTER TABLE local_ega.status OWNER TO postgres;

--
-- Name: errors; Type: TABLE; Schema: local_ega_download; Owner: postgres
--

CREATE TABLE local_ega_download.errors (
    id integer NOT NULL,
    req_id integer NOT NULL,
    code text NOT NULL,
    description text NOT NULL,
    hostname text,
    occured_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL
);


ALTER TABLE local_ega_download.errors OWNER TO postgres;

--
-- Name: errors_id_seq; Type: SEQUENCE; Schema: local_ega_download; Owner: postgres
--

CREATE SEQUENCE local_ega_download.errors_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER TABLE local_ega_download.errors_id_seq OWNER TO postgres;

--
-- Name: errors_id_seq; Type: SEQUENCE OWNED BY; Schema: local_ega_download; Owner: postgres
--

ALTER SEQUENCE local_ega_download.errors_id_seq OWNED BY local_ega_download.errors.id;


--
-- Name: requests; Type: TABLE; Schema: local_ega_download; Owner: postgres
--

CREATE TABLE local_ega_download.requests (
    id integer NOT NULL,
    file_id integer NOT NULL,
    start_coordinate bigint DEFAULT 0,
    end_coordinate bigint,
    user_info text,
    client_ip text,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL
);


ALTER TABLE local_ega_download.requests OWNER TO postgres;

--
-- Name: requests_id_seq; Type: SEQUENCE; Schema: local_ega_download; Owner: postgres
--

CREATE SEQUENCE local_ega_download.requests_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER TABLE local_ega_download.requests_id_seq OWNER TO postgres;

--
-- Name: requests_id_seq; Type: SEQUENCE OWNED BY; Schema: local_ega_download; Owner: postgres
--

ALTER SEQUENCE local_ega_download.requests_id_seq OWNED BY local_ega_download.requests.id;


--
-- Name: success; Type: TABLE; Schema: local_ega_download; Owner: postgres
--

CREATE TABLE local_ega_download.success (
    id integer NOT NULL,
    req_id integer NOT NULL,
    bytes bigint DEFAULT 0,
    speed double precision DEFAULT 0.0,
    occured_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL
);


ALTER TABLE local_ega_download.success OWNER TO postgres;

--
-- Name: success_id_seq; Type: SEQUENCE; Schema: local_ega_download; Owner: postgres
--

CREATE SEQUENCE local_ega_download.success_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER TABLE local_ega_download.success_id_seq OWNER TO postgres;

--
-- Name: success_id_seq; Type: SEQUENCE OWNED BY; Schema: local_ega_download; Owner: postgres
--

ALTER SEQUENCE local_ega_download.success_id_seq OWNED BY local_ega_download.success.id;


--
-- Name: file; Type: VIEW; Schema: local_ega_ebi; Owner: postgres
--

CREATE VIEW local_ega_ebi.file AS
 SELECT main.stable_id AS file_id,
    main.archive_file_reference AS file_name,
    main.archive_file_reference AS file_path,
    reverse(split_part(reverse(main.submission_file_path), '/'::text, 1)) AS display_file_name,
    main.archive_file_size AS file_size,
    NULL::text AS checksum,
    NULL::text AS checksum_type,
    main.archive_file_checksum AS unencrypted_checksum,
    main.archive_file_checksum_type AS unencrypted_checksum_type,
    main.status AS file_status,
    main.header
   FROM local_ega.main
  WHERE ((main.status)::text = 'READY'::text);


ALTER TABLE local_ega_ebi.file OWNER TO postgres;

--
-- Name: filedataset; Type: TABLE; Schema: local_ega_ebi; Owner: postgres
--

CREATE TABLE local_ega_ebi.filedataset (
    id integer NOT NULL,
    file_id integer NOT NULL,
    dataset_stable_id text NOT NULL
);


ALTER TABLE local_ega_ebi.filedataset OWNER TO postgres;

--
-- Name: file_dataset; Type: VIEW; Schema: local_ega_ebi; Owner: postgres
--

CREATE VIEW local_ega_ebi.file_dataset AS
 SELECT m.stable_id AS file_id,
    fd.dataset_stable_id AS dataset_id
   FROM (local_ega_ebi.filedataset fd
     JOIN local_ega.main m ON ((fd.file_id = m.id)));


ALTER TABLE local_ega_ebi.file_dataset OWNER TO postgres;

--
-- Name: fileindexfile; Type: TABLE; Schema: local_ega_ebi; Owner: postgres
--

CREATE TABLE local_ega_ebi.fileindexfile (
    id integer NOT NULL,
    file_id integer NOT NULL,
    index_file_id text,
    index_file_reference text NOT NULL,
    index_file_type local_ega.storage
);


ALTER TABLE local_ega_ebi.fileindexfile OWNER TO postgres;

--
-- Name: file_index_file; Type: VIEW; Schema: local_ega_ebi; Owner: postgres
--

CREATE VIEW local_ega_ebi.file_index_file AS
 SELECT m.stable_id AS file_id,
    fif.index_file_id
   FROM (local_ega_ebi.fileindexfile fif
     JOIN local_ega.main m ON ((fif.file_id = m.id)));


ALTER TABLE local_ega_ebi.file_index_file OWNER TO postgres;

--
-- Name: filedataset_id_seq; Type: SEQUENCE; Schema: local_ega_ebi; Owner: postgres
--

CREATE SEQUENCE local_ega_ebi.filedataset_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER TABLE local_ega_ebi.filedataset_id_seq OWNER TO postgres;

--
-- Name: filedataset_id_seq; Type: SEQUENCE OWNED BY; Schema: local_ega_ebi; Owner: postgres
--

ALTER SEQUENCE local_ega_ebi.filedataset_id_seq OWNED BY local_ega_ebi.filedataset.id;


--
-- Name: fileindexfile_id_seq; Type: SEQUENCE; Schema: local_ega_ebi; Owner: postgres
--

CREATE SEQUENCE local_ega_ebi.fileindexfile_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER TABLE local_ega_ebi.fileindexfile_id_seq OWNER TO postgres;

--
-- Name: fileindexfile_id_seq; Type: SEQUENCE OWNED BY; Schema: local_ega_ebi; Owner: postgres
--

ALTER SEQUENCE local_ega_ebi.fileindexfile_id_seq OWNED BY local_ega_ebi.fileindexfile.id;


--
-- Name: main id; Type: DEFAULT; Schema: local_ega; Owner: postgres
--

ALTER TABLE ONLY local_ega.main ALTER COLUMN id SET DEFAULT nextval('local_ega.main_id_seq'::regclass);


--
-- Name: main_errors id; Type: DEFAULT; Schema: local_ega; Owner: postgres
--

ALTER TABLE ONLY local_ega.main_errors ALTER COLUMN id SET DEFAULT nextval('local_ega.main_errors_id_seq'::regclass);


--
-- Name: errors id; Type: DEFAULT; Schema: local_ega_download; Owner: postgres
--

ALTER TABLE ONLY local_ega_download.errors ALTER COLUMN id SET DEFAULT nextval('local_ega_download.errors_id_seq'::regclass);


--
-- Name: requests id; Type: DEFAULT; Schema: local_ega_download; Owner: postgres
--

ALTER TABLE ONLY local_ega_download.requests ALTER COLUMN id SET DEFAULT nextval('local_ega_download.requests_id_seq'::regclass);


--
-- Name: success id; Type: DEFAULT; Schema: local_ega_download; Owner: postgres
--

ALTER TABLE ONLY local_ega_download.success ALTER COLUMN id SET DEFAULT nextval('local_ega_download.success_id_seq'::regclass);


--
-- Name: filedataset id; Type: DEFAULT; Schema: local_ega_ebi; Owner: postgres
--

ALTER TABLE ONLY local_ega_ebi.filedataset ALTER COLUMN id SET DEFAULT nextval('local_ega_ebi.filedataset_id_seq'::regclass);


--
-- Name: fileindexfile id; Type: DEFAULT; Schema: local_ega_ebi; Owner: postgres
--

ALTER TABLE ONLY local_ega_ebi.fileindexfile ALTER COLUMN id SET DEFAULT nextval('local_ega_ebi.fileindexfile_id_seq'::regclass);


--
-- Data for Name: archive_encryption; Type: TABLE DATA; Schema: local_ega; Owner: postgres
--

COPY local_ega.archive_encryption (mode, description) FROM stdin;
CRYPT4GH	Crypt4GH encryption (using version)
PGP	OpenPGP encryption (RFC 4880)
AES	AES encryption with passphrase
CUSTOM1	Custom method 1 for local site
CUSTOM2	Custom method 2 for local site
\.


--
-- Data for Name: dbschema_version; Type: TABLE DATA; Schema: local_ega; Owner: postgres
--

COPY local_ega.dbschema_version (version, applied, description) FROM stdin;
0	2023-09-20 09:54:13.647297+00	Created with version
\.


--
-- Data for Name: main; Type: TABLE DATA; Schema: local_ega; Owner: postgres
--

COPY local_ega.main (id, stable_id, status, submission_file_path, submission_file_extension, submission_file_calculated_checksum, submission_file_calculated_checksum_type, submission_file_size, submission_user, archive_file_reference, archive_file_type, archive_file_size, archive_file_checksum, archive_file_checksum_type, encryption_method, version, header, created_by, last_modified_by, created_at, last_modified) FROM stdin;
\.


--
-- Data for Name: main_errors; Type: TABLE DATA; Schema: local_ega; Owner: postgres
--

COPY local_ega.main_errors (id, active, file_id, hostname, error_type, msg, from_user, occured_at) FROM stdin;
\.


--
-- Data for Name: session_key_checksums_sha256; Type: TABLE DATA; Schema: local_ega; Owner: postgres
--

COPY local_ega.session_key_checksums_sha256 (session_key_checksum, session_key_checksum_type, file_id) FROM stdin;
\.


--
-- Data for Name: status; Type: TABLE DATA; Schema: local_ega; Owner: postgres
--

COPY local_ega.status (id, code, description) FROM stdin;
10	INIT	Initializing a file ingestion
20	IN_INGESTION	Currently under ingestion
30	ARCHIVED	File moved to Archive
40	COMPLETED	File verified in Archive
50	READY	File ingested, ready for download
0	ERROR	An Error occured, check the error table
1	DISABLED	Used for submissions that are stopped, overwritten, or to be cleaned up
\.


--
-- Data for Name: errors; Type: TABLE DATA; Schema: local_ega_download; Owner: postgres
--

COPY local_ega_download.errors (id, req_id, code, description, hostname, occured_at) FROM stdin;
\.


--
-- Data for Name: requests; Type: TABLE DATA; Schema: local_ega_download; Owner: postgres
--

COPY local_ega_download.requests (id, file_id, start_coordinate, end_coordinate, user_info, client_ip, created_at) FROM stdin;
\.


--
-- Data for Name: success; Type: TABLE DATA; Schema: local_ega_download; Owner: postgres
--

COPY local_ega_download.success (id, req_id, bytes, speed, occured_at) FROM stdin;
\.


--
-- Data for Name: filedataset; Type: TABLE DATA; Schema: local_ega_ebi; Owner: postgres
--

COPY local_ega_ebi.filedataset (id, file_id, dataset_stable_id) FROM stdin;
\.


--
-- Data for Name: fileindexfile; Type: TABLE DATA; Schema: local_ega_ebi; Owner: postgres
--

COPY local_ega_ebi.fileindexfile (id, file_id, index_file_id, index_file_reference, index_file_type) FROM stdin;
\.


--
-- Name: main_errors_id_seq; Type: SEQUENCE SET; Schema: local_ega; Owner: postgres
--

SELECT pg_catalog.setval('local_ega.main_errors_id_seq', 1, false);


--
-- Name: main_id_seq; Type: SEQUENCE SET; Schema: local_ega; Owner: postgres
--

SELECT pg_catalog.setval('local_ega.main_id_seq', 1, false);


--
-- Name: errors_id_seq; Type: SEQUENCE SET; Schema: local_ega_download; Owner: postgres
--

SELECT pg_catalog.setval('local_ega_download.errors_id_seq', 1, false);


--
-- Name: requests_id_seq; Type: SEQUENCE SET; Schema: local_ega_download; Owner: postgres
--

SELECT pg_catalog.setval('local_ega_download.requests_id_seq', 1, false);


--
-- Name: success_id_seq; Type: SEQUENCE SET; Schema: local_ega_download; Owner: postgres
--

SELECT pg_catalog.setval('local_ega_download.success_id_seq', 1, false);


--
-- Name: filedataset_id_seq; Type: SEQUENCE SET; Schema: local_ega_ebi; Owner: postgres
--

SELECT pg_catalog.setval('local_ega_ebi.filedataset_id_seq', 1, false);


--
-- Name: fileindexfile_id_seq; Type: SEQUENCE SET; Schema: local_ega_ebi; Owner: postgres
--

SELECT pg_catalog.setval('local_ega_ebi.fileindexfile_id_seq', 1, false);


--
-- Name: archive_encryption archive_encryption_pkey; Type: CONSTRAINT; Schema: local_ega; Owner: postgres
--

ALTER TABLE ONLY local_ega.archive_encryption
    ADD CONSTRAINT archive_encryption_pkey PRIMARY KEY (mode);


--
-- Name: dbschema_version dbschema_version_pkey; Type: CONSTRAINT; Schema: local_ega; Owner: postgres
--

ALTER TABLE ONLY local_ega.dbschema_version
    ADD CONSTRAINT dbschema_version_pkey PRIMARY KEY (version);


--
-- Name: main_errors main_errors_pkey; Type: CONSTRAINT; Schema: local_ega; Owner: postgres
--

ALTER TABLE ONLY local_ega.main_errors
    ADD CONSTRAINT main_errors_pkey PRIMARY KEY (id);


--
-- Name: main main_pkey; Type: CONSTRAINT; Schema: local_ega; Owner: postgres
--

ALTER TABLE ONLY local_ega.main
    ADD CONSTRAINT main_pkey PRIMARY KEY (id);


--
-- Name: session_key_checksums_sha256 session_key_checksums_sha256_pkey; Type: CONSTRAINT; Schema: local_ega; Owner: postgres
--

ALTER TABLE ONLY local_ega.session_key_checksums_sha256
    ADD CONSTRAINT session_key_checksums_sha256_pkey PRIMARY KEY (session_key_checksum);


--
-- Name: status status_code_key; Type: CONSTRAINT; Schema: local_ega; Owner: postgres
--

ALTER TABLE ONLY local_ega.status
    ADD CONSTRAINT status_code_key UNIQUE (code);


--
-- Name: status status_pkey; Type: CONSTRAINT; Schema: local_ega; Owner: postgres
--

ALTER TABLE ONLY local_ega.status
    ADD CONSTRAINT status_pkey PRIMARY KEY (id);


--
-- Name: errors errors_pkey; Type: CONSTRAINT; Schema: local_ega_download; Owner: postgres
--

ALTER TABLE ONLY local_ega_download.errors
    ADD CONSTRAINT errors_pkey PRIMARY KEY (id);


--
-- Name: requests requests_pkey; Type: CONSTRAINT; Schema: local_ega_download; Owner: postgres
--

ALTER TABLE ONLY local_ega_download.requests
    ADD CONSTRAINT requests_pkey PRIMARY KEY (id);


--
-- Name: success success_pkey; Type: CONSTRAINT; Schema: local_ega_download; Owner: postgres
--

ALTER TABLE ONLY local_ega_download.success
    ADD CONSTRAINT success_pkey PRIMARY KEY (id);


--
-- Name: filedataset filedataset_pkey; Type: CONSTRAINT; Schema: local_ega_ebi; Owner: postgres
--

ALTER TABLE ONLY local_ega_ebi.filedataset
    ADD CONSTRAINT filedataset_pkey PRIMARY KEY (id);


--
-- Name: fileindexfile fileindexfile_pkey; Type: CONSTRAINT; Schema: local_ega_ebi; Owner: postgres
--

ALTER TABLE ONLY local_ega_ebi.fileindexfile
    ADD CONSTRAINT fileindexfile_pkey PRIMARY KEY (id);


--
-- Name: file_id_idx; Type: INDEX; Schema: local_ega; Owner: postgres
--

CREATE UNIQUE INDEX file_id_idx ON local_ega.main USING btree (id);


--
-- Name: main main_updated; Type: TRIGGER; Schema: local_ega; Owner: postgres
--

CREATE TRIGGER main_updated AFTER UPDATE ON local_ega.main FOR EACH ROW EXECUTE FUNCTION local_ega.main_updated();


--
-- Name: main mark_ready; Type: TRIGGER; Schema: local_ega; Owner: postgres
--

CREATE TRIGGER mark_ready AFTER UPDATE OF status ON local_ega.main FOR EACH ROW WHEN (((new.status)::text = 'READY'::text)) EXECUTE FUNCTION local_ega.mark_ready();


--
-- Name: main main_encryption_method_fkey; Type: FK CONSTRAINT; Schema: local_ega; Owner: postgres
--

ALTER TABLE ONLY local_ega.main
    ADD CONSTRAINT main_encryption_method_fkey FOREIGN KEY (encryption_method) REFERENCES local_ega.archive_encryption(mode);


--
-- Name: main_errors main_errors_file_id_fkey; Type: FK CONSTRAINT; Schema: local_ega; Owner: postgres
--

ALTER TABLE ONLY local_ega.main_errors
    ADD CONSTRAINT main_errors_file_id_fkey FOREIGN KEY (file_id) REFERENCES local_ega.main(id) ON DELETE CASCADE;


--
-- Name: main main_status_fkey; Type: FK CONSTRAINT; Schema: local_ega; Owner: postgres
--

ALTER TABLE ONLY local_ega.main
    ADD CONSTRAINT main_status_fkey FOREIGN KEY (status) REFERENCES local_ega.status(code);


--
-- Name: session_key_checksums_sha256 session_key_checksums_sha256_file_id_fkey; Type: FK CONSTRAINT; Schema: local_ega; Owner: postgres
--

ALTER TABLE ONLY local_ega.session_key_checksums_sha256
    ADD CONSTRAINT session_key_checksums_sha256_file_id_fkey FOREIGN KEY (file_id) REFERENCES local_ega.main(id) ON DELETE CASCADE;


--
-- Name: errors errors_req_id_fkey; Type: FK CONSTRAINT; Schema: local_ega_download; Owner: postgres
--

ALTER TABLE ONLY local_ega_download.errors
    ADD CONSTRAINT errors_req_id_fkey FOREIGN KEY (req_id) REFERENCES local_ega_download.requests(id);


--
-- Name: requests requests_file_id_fkey; Type: FK CONSTRAINT; Schema: local_ega_download; Owner: postgres
--

ALTER TABLE ONLY local_ega_download.requests
    ADD CONSTRAINT requests_file_id_fkey FOREIGN KEY (file_id) REFERENCES local_ega.main(id);


--
-- Name: success success_req_id_fkey; Type: FK CONSTRAINT; Schema: local_ega_download; Owner: postgres
--

ALTER TABLE ONLY local_ega_download.success
    ADD CONSTRAINT success_req_id_fkey FOREIGN KEY (req_id) REFERENCES local_ega_download.requests(id);


--
-- Name: filedataset filedataset_file_id_fkey; Type: FK CONSTRAINT; Schema: local_ega_ebi; Owner: postgres
--

ALTER TABLE ONLY local_ega_ebi.filedataset
    ADD CONSTRAINT filedataset_file_id_fkey FOREIGN KEY (file_id) REFERENCES local_ega.main(id) ON DELETE CASCADE;


--
-- Name: fileindexfile fileindexfile_file_id_fkey; Type: FK CONSTRAINT; Schema: local_ega_ebi; Owner: postgres
--

ALTER TABLE ONLY local_ega_ebi.fileindexfile
    ADD CONSTRAINT fileindexfile_file_id_fkey FOREIGN KEY (file_id) REFERENCES local_ega.main(id) ON DELETE CASCADE;


--
-- Name: SCHEMA local_ega; Type: ACL; Schema: -; Owner: postgres
--

GRANT USAGE ON SCHEMA local_ega TO lega_in;
GRANT USAGE ON SCHEMA local_ega TO lega_out;


--
-- Name: SCHEMA local_ega_download; Type: ACL; Schema: -; Owner: postgres
--

GRANT USAGE ON SCHEMA local_ega_download TO lega_out;


--
-- Name: SCHEMA local_ega_ebi; Type: ACL; Schema: -; Owner: postgres
--

GRANT USAGE ON SCHEMA local_ega_ebi TO lega_out;


--
-- Name: TABLE archive_encryption; Type: ACL; Schema: local_ega; Owner: postgres
--

GRANT ALL ON TABLE local_ega.archive_encryption TO lega_in;


--
-- Name: TABLE main; Type: ACL; Schema: local_ega; Owner: postgres
--

GRANT ALL ON TABLE local_ega.main TO lega_in;


--
-- Name: TABLE archive_files; Type: ACL; Schema: local_ega; Owner: postgres
--

GRANT ALL ON TABLE local_ega.archive_files TO lega_in;
GRANT SELECT ON TABLE local_ega.archive_files TO lega_out;


--
-- Name: TABLE dbschema_version; Type: ACL; Schema: local_ega; Owner: postgres
--

GRANT ALL ON TABLE local_ega.dbschema_version TO lega_in;


--
-- Name: TABLE main_errors; Type: ACL; Schema: local_ega; Owner: postgres
--

GRANT ALL ON TABLE local_ega.main_errors TO lega_in;


--
-- Name: TABLE errors; Type: ACL; Schema: local_ega; Owner: postgres
--

GRANT ALL ON TABLE local_ega.errors TO lega_in;


--
-- Name: TABLE files; Type: ACL; Schema: local_ega; Owner: postgres
--

GRANT ALL ON TABLE local_ega.files TO lega_in;


--
-- Name: SEQUENCE main_errors_id_seq; Type: ACL; Schema: local_ega; Owner: postgres
--

GRANT ALL ON SEQUENCE local_ega.main_errors_id_seq TO lega_in;


--
-- Name: SEQUENCE main_id_seq; Type: ACL; Schema: local_ega; Owner: postgres
--

GRANT ALL ON SEQUENCE local_ega.main_id_seq TO lega_in;


--
-- Name: TABLE session_key_checksums_sha256; Type: ACL; Schema: local_ega; Owner: postgres
--

GRANT ALL ON TABLE local_ega.session_key_checksums_sha256 TO lega_in;


--
-- Name: TABLE status; Type: ACL; Schema: local_ega; Owner: postgres
--

GRANT ALL ON TABLE local_ega.status TO lega_in;


--
-- Name: TABLE errors; Type: ACL; Schema: local_ega_download; Owner: postgres
--

GRANT ALL ON TABLE local_ega_download.errors TO lega_out;


--
-- Name: SEQUENCE errors_id_seq; Type: ACL; Schema: local_ega_download; Owner: postgres
--

GRANT ALL ON SEQUENCE local_ega_download.errors_id_seq TO lega_out;


--
-- Name: TABLE requests; Type: ACL; Schema: local_ega_download; Owner: postgres
--

GRANT ALL ON TABLE local_ega_download.requests TO lega_out;


--
-- Name: SEQUENCE requests_id_seq; Type: ACL; Schema: local_ega_download; Owner: postgres
--

GRANT ALL ON SEQUENCE local_ega_download.requests_id_seq TO lega_out;


--
-- Name: TABLE success; Type: ACL; Schema: local_ega_download; Owner: postgres
--

GRANT ALL ON TABLE local_ega_download.success TO lega_out;


--
-- Name: SEQUENCE success_id_seq; Type: ACL; Schema: local_ega_download; Owner: postgres
--

GRANT ALL ON SEQUENCE local_ega_download.success_id_seq TO lega_out;


--
-- Name: TABLE file; Type: ACL; Schema: local_ega_ebi; Owner: postgres
--

GRANT ALL ON TABLE local_ega_ebi.file TO lega_out;


--
-- Name: TABLE filedataset; Type: ACL; Schema: local_ega_ebi; Owner: postgres
--

GRANT ALL ON TABLE local_ega_ebi.filedataset TO lega_out;


--
-- Name: TABLE file_dataset; Type: ACL; Schema: local_ega_ebi; Owner: postgres
--

GRANT ALL ON TABLE local_ega_ebi.file_dataset TO lega_out;


--
-- Name: TABLE fileindexfile; Type: ACL; Schema: local_ega_ebi; Owner: postgres
--

GRANT ALL ON TABLE local_ega_ebi.fileindexfile TO lega_out;


--
-- Name: TABLE file_index_file; Type: ACL; Schema: local_ega_ebi; Owner: postgres
--

GRANT ALL ON TABLE local_ega_ebi.file_index_file TO lega_out;


--
-- Name: SEQUENCE filedataset_id_seq; Type: ACL; Schema: local_ega_ebi; Owner: postgres
--

GRANT ALL ON SEQUENCE local_ega_ebi.filedataset_id_seq TO lega_out;


--
-- Name: SEQUENCE fileindexfile_id_seq; Type: ACL; Schema: local_ega_ebi; Owner: postgres
--

GRANT ALL ON SEQUENCE local_ega_ebi.fileindexfile_id_seq TO lega_out;


--
-- PostgreSQL database dump complete
--

\unrestrict kPohhoV0ak2tqryoKWXZcssxFfxjWhlVXmKFZTo1yb1HCuc3vfI2bA7nViJjO3Y

