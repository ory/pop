package pop

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"os/user"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func Test_PostgreSQL_ConnectionDetails_Values_Finalize(t *testing.T) {
	r := require.New(t)

	cd := &ConnectionDetails{
		Dialect:  "postgres",
		Database: "database",
		Host:     "host",
		Port:     "1234",
		User:     "user",
		Password: "pass#",
	}
	err := cd.Finalize()
	r.NoError(err)

	p := &postgresql{commonDialect: commonDialect{ConnectionDetails: cd}}

	r.Equal("postgres://user:pass%23@host:1234/database?", p.URL())
}

func Test_PostgreSQL_Connection_String(t *testing.T) {
	r := require.New(t)

	url := "host=host port=1234 dbname=database user=user password=pass#"
	cd := &ConnectionDetails{
		Dialect: "postgres",
		URL:     url,
	}
	err := cd.Finalize()
	r.NoError(err)

	r.Equal(url, cd.URL)
	r.Equal("postgres", cd.Dialect)
	r.Equal("host", cd.Host)
	r.Equal("pass#", cd.Password)
	r.Equal("1234", cd.Port)
	r.Equal("user", cd.User)
	r.Equal("database", cd.Database)
}

func generateTestCerts(t *testing.T, dir string) (sslcert, sslkey, sslrootcert string) {
	// Generate CA key pair
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	require.NoError(t, err)

	// Generate client key pair
	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	clientTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "test-client"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	caCert, err := x509.ParseCertificate(caDER)
	require.NoError(t, err)

	clientDER, err := x509.CreateCertificate(rand.Reader, clientTemplate, caCert, &clientKey.PublicKey, caKey)
	require.NoError(t, err)

	// Marshal client private key to DER
	clientKeyDER, err := x509.MarshalECPrivateKey(clientKey)
	require.NoError(t, err)

	// Write files
	sslrootcert = filepath.Join(dir, "root.crt")
	sslcert = filepath.Join(dir, "client.crt")
	sslkey = filepath.Join(dir, "client.key")

	writePEM(t, sslrootcert, "CERTIFICATE", caDER)
	writePEM(t, sslcert, "CERTIFICATE", clientDER)
	writePEM(t, sslkey, "EC PRIVATE KEY", clientKeyDER)

	return sslcert, sslkey, sslrootcert
}

func writePEM(t *testing.T, path, pemType string, der []byte) {
	t.Helper()
	f, err := os.Create(path)
	require.NoError(t, err)
	defer f.Close()
	require.NoError(t, pem.Encode(f, &pem.Block{Type: pemType, Bytes: der}))
}

func Test_PostgreSQL_Connection_String_Options(t *testing.T) {
	r := require.New(t)

	sslcert, sslkey, sslrootcert := generateTestCerts(t, t.TempDir())
	url := fmt.Sprintf("host=host port=1234 dbname=database user=user password=pass# sslmode=disable fallback_application_name=test_app connect_timeout=10 sslcert=%s sslkey=%s sslrootcert=%s", sslcert, sslkey, sslrootcert)
	cd := &ConnectionDetails{
		Dialect: "postgres",
		URL:     url,
	}
	err := cd.Finalize()
	r.NoError(err)

	r.Equal(url, cd.URL)

	r.Equal("disable", cd.Options["sslmode"])
	r.Equal("test_app", cd.Options["fallback_application_name"])
}

func Test_PostgreSQL_Connection_String_Without_User(t *testing.T) {
	r := require.New(t)

	url := "dbname=database"
	cd := &ConnectionDetails{
		Dialect: "postgres",
		URL:     url,
	}
	err := cd.Finalize()
	r.NoError(err)

	uc := os.Getenv("PGUSER")
	if uc == "" {
		c, err := user.Current()
		if err == nil {
			uc = c.Username
		}
	}

	r.Equal(url, cd.URL)
	r.Equal("postgres", cd.Dialect)

	var foundHost bool
	for _, host := range []string{
		"/var/run/postgresql", // Debian
		"/private/tmp",        // OSX - homebrew
		"/tmp",                // standard PostgreSQL
		"localhost",           // Windows does not do sockets
	} {
		if cd.Host == host {
			foundHost = true
			break
		}
	}
	r.True(foundHost, `Got host: "%s"`, cd.Host)

	r.Equal(os.Getenv("PGPASSWORD"), cd.Password)
	r.Equal(portPostgreSQL, cd.Port) // fallback
	r.Equal(uc, cd.User)
	r.Equal("database", cd.Database)
}

func Test_PostgreSQL_Connection_String_Failure(t *testing.T) {
	r := require.New(t)

	url := "abc"
	cd := &ConnectionDetails{
		Dialect: "postgres",
		URL:     url,
	}
	err := cd.Finalize()
	r.Error(err)
	r.Equal("postgres", cd.Dialect)
}

func Test_PostgreSQL_Quotable(t *testing.T) {
	r := require.New(t)
	p := postgresql{}

	r.Equal(`"table_name"`, p.Quote("table_name"))
	r.Equal(`"schema"."table_name"`, p.Quote("schema.table_name"))
	r.Equal(`"schema"."table name"`, p.Quote(`"schema"."table name"`))
}
