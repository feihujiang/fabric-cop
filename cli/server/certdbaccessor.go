/*
Copyright IBM Corp. 2016 All Rights Reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

                 http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package server

import (
	"errors"
	"fmt"
	"time"

	"github.com/cloudflare/cfssl/certdb"
	certsql "github.com/cloudflare/cfssl/certdb/sql"
	"github.com/cloudflare/cfssl/log"
	cop "github.com/hyperledger/fabric-cop/api"
	"github.com/hyperledger/fabric-cop/util"
	"github.com/kisielk/sqlstruct"

	"github.com/jmoiron/sqlx"
)

const (
	insertSQL = `
INSERT INTO certificates (id, serial_number, authority_key_identifier, ca_label, status, reason, expiry, revoked_at, pem)
	VALUES (:id, :serial_number, :authority_key_identifier, :ca_label, :status, :reason, :expiry, :revoked_at, :pem);`

	selectSQL = `
SELECT %s FROM certificates
WHERE (id = ?);`

	updateRevokeSQL = `
UPDATE certificates
SET status='revoked', revoked_at=CURRENT_TIMESTAMP, reason=:reason
WHERE (id = :id AND status != 'revoked');`
)

// CertRecord extends CFSSL CertificateRecord by adding an enrollment ID to the record
type CertRecord struct {
	ID string `db:"id"`
	certdb.CertificateRecord
}

// CertDBAccessor implements certdb.Accessor interface.
type CertDBAccessor struct {
	accessor certdb.Accessor
	db       *sqlx.DB
}

// NewCertDBAccessor returns a new Accessor.
func NewCertDBAccessor(db *sqlx.DB) *CertDBAccessor {
	cffslAcc := new(CertDBAccessor)
	cffslAcc.db = db
	cffslAcc.accessor = certsql.NewAccessor(db)
	return cffslAcc
}

func (d *CertDBAccessor) checkDB() error {
	if d.db == nil {
		return errors.New("Error")
	}
	return nil
}

// SetDB changes the underlying sql.DB object Accessor is manipulating.
func (d *CertDBAccessor) SetDB(db *sqlx.DB) {
	d.db = db
	return
}

// InsertCertificate puts a CertificateRecord into db.
func (d *CertDBAccessor) InsertCertificate(cr certdb.CertificateRecord) error {

	log.Debug("DB: Insert Certificate")

	err := d.checkDB()
	if err != nil {
		return err
	}
	id, err := util.GetEnrollmentIDFromPEM([]byte(cr.PEM))

	err = d.checkDB()
	if err != nil {
		return err
	}

	var record = new(CertRecord)
	record.ID = id
	record.Serial = cr.Serial
	record.AKI = cr.AKI
	record.CALabel = cr.CALabel
	record.Status = cr.Status
	record.Reason = cr.Reason
	record.Expiry = cr.Expiry.UTC()
	record.RevokedAt = cr.RevokedAt.UTC()
	record.PEM = cr.PEM

	res, err := d.db.NamedExec(insertSQL, record)
	if err != nil {
		log.Errorf("Error occured during insertion of record [error: %s]", err)
		return err
	}

	numRowsAffected, err := res.RowsAffected()

	if numRowsAffected == 0 {
		// log.Errorf("Failed to insert the certificate record")
		return cop.NewError(cop.DatabaseError, "Failed to insert the certificate record")
	}

	if numRowsAffected != 1 {
		// log.Errorf("%d rows are affected, should be 1 row", numRowsAffected)
		return cop.NewError(cop.DatabaseError, "%d rows are affected, should be 1 row", numRowsAffected)
	}

	return err
}

// GetCertificatesByID gets a CertificateRecord indexed by id.
func (d *CertDBAccessor) GetCertificatesByID(id string) (crs []CertRecord, err error) {
	log.Debugf("DB: Get certificate by ID (%s)", id)
	err = d.checkDB()
	if err != nil {
		return nil, err
	}

	err = d.db.Select(&crs, fmt.Sprintf(d.db.Rebind(selectSQL), sqlstruct.Columns(CertRecord{})), id)
	if err != nil {
		return nil, err
	}

	return crs, nil
}

// GetCertificate gets a CertificateRecord indexed by serial.
func (d *CertDBAccessor) GetCertificate(serial, aki string) (crs []certdb.CertificateRecord, err error) {
	crs, err = d.accessor.GetCertificate(serial, aki)
	if err != nil {
		return nil, err
	}
	return crs, nil
}

// GetUnexpiredCertificates gets all unexpired certificate from db.
func (d *CertDBAccessor) GetUnexpiredCertificates() (crs []certdb.CertificateRecord, err error) {
	crs, err = d.accessor.GetUnexpiredCertificates()
	if err != nil {
		return nil, err
	}
	return crs, err
}

// RevokeCertificatesByID updates all certificates for a given ID and marks them revoked.
func (d *CertDBAccessor) RevokeCertificatesByID(id string, reasonCode int) (crs []CertRecord, err error) {
	err = d.checkDB()
	if err != nil {
		return nil, err
	}

	var record = new(CertRecord)
	record.ID = id
	record.Reason = reasonCode

	err = d.db.Select(&crs, d.db.Rebind("SELECT * FROM certificates WHERE (id = ? AND status != 'revoked')"), id)
	if err != nil {
		return nil, err
	}

	_, err = d.db.NamedExec(updateRevokeSQL, record)
	if err != nil {
		return nil, err
	}

	return crs, err
}

// RevokeCertificate updates a certificate with a given serial number and marks it revoked.
func (d *CertDBAccessor) RevokeCertificate(serial, aki string, reasonCode int) error {
	err := d.accessor.RevokeCertificate(serial, aki, reasonCode)
	return err
}

// InsertOCSP puts a new certdb.OCSPRecord into the db.
func (d *CertDBAccessor) InsertOCSP(rr certdb.OCSPRecord) error {
	err := d.accessor.InsertOCSP(rr)
	return err
}

// GetOCSP retrieves a certdb.OCSPRecord from db by serial.
func (d *CertDBAccessor) GetOCSP(serial, aki string) (ors []certdb.OCSPRecord, err error) {
	ors, err = d.accessor.GetOCSP(serial, aki)
	return ors, err
}

// GetUnexpiredOCSPs retrieves all unexpired certdb.OCSPRecord from db.
func (d *CertDBAccessor) GetUnexpiredOCSPs() (ors []certdb.OCSPRecord, err error) {
	ors, err = d.accessor.GetUnexpiredOCSPs()
	return ors, err
}

// UpdateOCSP updates a ocsp response record with a given serial number.
func (d *CertDBAccessor) UpdateOCSP(serial, aki, body string, expiry time.Time) error {
	err := d.accessor.UpdateOCSP(serial, aki, body, expiry)
	return err
}

// UpsertOCSP update a ocsp response record with a given serial number,
// or insert the record if it doesn't yet exist in the db
func (d *CertDBAccessor) UpsertOCSP(serial, aki, body string, expiry time.Time) error {
	err := d.accessor.UpsertOCSP(serial, aki, body, expiry)
	return err
}
