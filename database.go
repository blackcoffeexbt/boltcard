package main

import (
	"database/sql"
	"errors"
	"fmt"
	_ "github.com/lib/pq"
	"os"
)

type card struct {
	card_id                    int
	card_guid                  string
	aes_dec                    string
	aes_cmac                   string
	db_uid                     string
	last_counter_value         uint32
	lnurlw_request_timeout_sec int
	enable_flag                string
	tx_limit_sats              int
	day_limit_sats             int
	one_time_code	string
	lock_key	string
}

type payment struct {
	card_payment_id int
	card_id         int
	k1              string
	paid_flag       string
}

func db_open() (*sql.DB, error) {

	// get connection string from environment variables

	conn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		os.Getenv("DB_HOST"),
		os.Getenv("DB_PORT"),
		os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_NAME"))

	db, err := sql.Open("postgres", conn)
	if err != nil {
		return db, err
	}

	return db, nil
}

func db_get_new_card(one_time_code string) (*card, error) {
	c := card{}

	db, err := db_open()
	if err != nil {
		return &c, err
	}
	defer db.Close()

	sqlStatement := `SELECT lock_key, aes_cmac` +
		` FROM cards WHERE one_time_code=$1 AND` +
		` one_time_code_expiry > NOW() AND one_time_code_used = 'N';`
	row := db.QueryRow(sqlStatement, one_time_code)
	err = row.Scan(
		&c.lock_key,
		&c.aes_cmac)
	if err != nil {
		return &c, err
	}

	sqlStatement = `UPDATE cards SET one_time_code_used = 'Y' WHERE one_time_code = $1;`
	_, err = db.Exec(sqlStatement, one_time_code)
	if err != nil {
		return &c, err
	}

	return &c, nil
}

func db_get_card_from_uid(card_uid string) (*card, error) {

	c := card{}

	db, err := db_open()
	if err != nil {
		return &c, err
	}
	defer db.Close()

	sqlStatement := `SELECT card_id, aes_cmac, uid,` +
		` last_counter_value, lnurlw_request_timeout_sec,` +
		` enable_flag, tx_limit_sats, day_limit_sats` +
		` FROM cards WHERE uid=$1;`
	row := db.QueryRow(sqlStatement, card_uid)
	err = row.Scan(
		&c.card_id,
		&c.aes_cmac,
		&c.db_uid,
		&c.last_counter_value,
		&c.lnurlw_request_timeout_sec,
		&c.enable_flag,
		&c.tx_limit_sats,
		&c.day_limit_sats)
	if err != nil {
		return &c, err
	}

	return &c, nil
}

func db_get_card_from_card_id(card_id int) (*card, error) {

	c := card{}

	db, err := db_open()
	if err != nil {
		return &c, err
	}
	defer db.Close()

	sqlStatement := `SELECT card_id, aes_cmac, uid,` +
		` last_counter_value, lnurlw_request_timeout_sec,` +
		` enable_flag, tx_limit_sats, day_limit_sats` +
		` FROM cards WHERE card_id=$1;`
	row := db.QueryRow(sqlStatement, card_id)
	err = row.Scan(
		&c.card_id,
		&c.aes_cmac,
		&c.db_uid,
		&c.last_counter_value,
		&c.lnurlw_request_timeout_sec,
		&c.enable_flag,
		&c.tx_limit_sats,
		&c.day_limit_sats)
	if err != nil {
		return &c, err
	}

	return &c, nil
}

func db_check_lnurlw_timeout(card_payment_id int) (bool, error) {

	db, err := db_open()
	if err != nil {
		return true, err
	}
	defer db.Close()

	lnurlw_timeout := true

	sqlStatement := `SELECT NOW() > cp.lnurlw_request_time + c.lnurlw_request_timeout_sec * INTERVAL '1 SECOND'` +
		` FROM  card_payments AS cp INNER JOIN cards AS c ON c.card_id = cp.card_id` +
		` WHERE cp.card_payment_id=$1;`
	row := db.QueryRow(sqlStatement, card_payment_id)
	err = row.Scan(&lnurlw_timeout)
	if err != nil {
		return true, err
	}

	return lnurlw_timeout, nil
}

func db_check_and_update_counter(card_id int, new_counter_value uint32) (bool, error) {

	db, err := db_open()
	if err != nil {
		return false, err
	}
	defer db.Close()

	sqlStatement := `UPDATE cards SET last_counter_value = $2 WHERE card_id = $1` +
		` AND last_counter_value < $2;`
	res, err := db.Exec(sqlStatement, card_id, new_counter_value)
	if err != nil {
		return false, err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if count != 1 {
		return false, nil
	}

	return true, nil
}

func db_insert_payment(card_id int, k1 string) error {

	db, err := db_open()
	if err != nil {
		return err
	}
	defer db.Close()

	// insert a new record into card_payments with card_id & k1 set

	sqlStatement := `INSERT INTO card_payments` +
		` (card_id, k1, paid_flag, lnurlw_request_time)` +
		` VALUES ($1, $2, 'N', NOW());`
	res, err := db.Exec(sqlStatement, card_id, k1)
	if err != nil {
		return err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("not one card_payments record inserted")
	}

	return nil
}

func db_get_payment_k1(k1 string) (*payment, error) {
	p := payment{}

	db, err := db_open()
	if err != nil {
		return &p, err
	}
	defer db.Close()

	sqlStatement := `SELECT card_payment_id, card_id, paid_flag` +
		` FROM card_payments WHERE k1=$1;`
	row := db.QueryRow(sqlStatement, k1)
	err = row.Scan(
		&p.card_payment_id,
		&p.card_id,
		&p.paid_flag)
	if err != nil {
		return &p, err
	}

	return &p, nil
}

func db_update_payment_invoice(card_payment_id int, ln_invoice string, amount_msats int64) error {

	db, err := db_open()
	if err != nil {
		return err
	}
	defer db.Close()

	sqlStatement := `UPDATE card_payments SET ln_invoice = $2, amount_msats = $3 WHERE card_payment_id = $1;`
	res, err := db.Exec(sqlStatement, card_payment_id, ln_invoice, amount_msats)
	if err != nil {
		return err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("not one card_payment record updated")
	}

	return nil
}

func db_update_payment_paid(card_payment_id int) error {

	db, err := db_open()
	if err != nil {
		return err
	}
	defer db.Close()

	sqlStatement := `UPDATE card_payments SET paid_flag = 'Y', payment_time = NOW() WHERE card_payment_id = $1;`
	res, err := db.Exec(sqlStatement, card_payment_id)
	if err != nil {
		return err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("not one card_payment record updated")
	}

	return nil
}

func db_update_payment_status(card_payment_id int, payment_status string, failure_reason string) error {

	db, err := db_open()

	if err != nil {
		return err
	}

	defer db.Close()

	sqlStatement := `UPDATE card_payments SET payment_status = $2, failure_reason = $3, ` +
		`payment_status_time = NOW() WHERE card_payment_id = $1;`

	res, err := db.Exec(sqlStatement, card_payment_id, payment_status, failure_reason)

	if err != nil {
		return err
	}

	count, err := res.RowsAffected()

	if err != nil {
		return err
	}

	if count != 1 {
		return errors.New("not one card_payment record updated")
	}

	return nil
}

func db_get_card_totals(card_id int) (int, error) {

	db, err := db_open()
	if err != nil {
		return 0, err
	}
	defer db.Close()

	day_total_msats := 0

	sqlStatement := `SELECT COALESCE(SUM(amount_msats),0) FROM card_payments ` +
		`WHERE card_id=$1 AND paid_flag='Y' ` +
		`AND payment_time > NOW() - INTERVAL '1 DAY';`
	row := db.QueryRow(sqlStatement, card_id)
	err = row.Scan(&day_total_msats)
	if err != nil {
		return 0, err
	}

	day_total_sats := day_total_msats / 1000

	return day_total_sats, nil
}
