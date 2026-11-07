package redact

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

const passwordMark = "[REDACTED:password]"

// Keys whose last part is PASS or PWD, a pass or pwd key alone where it can
// only name a password, a MySQL or MariaDB client's -p<password>, curl's
// user:password and a password written in prose are replaced, and only the
// password. Each of these got through before redact.Version 5.
func TestPasswordShapes(t *testing.T) {
	isolate(t)
	pw := j("S3cr3t", "Pw9")
	pw2 := j("Pr0xy", "Pw99")
	spaced := j("S3cr3t", " Pw9")
	for _, c := range []struct{ name, in, want string }{
		// Keys whose last part is PASS or PWD.
		{"env pass", "DB_PASS=" + pw, "DB_PASS=" + secretMark},
		{"env pwd", "MYSQL_PWD=" + pw, "MYSQL_PWD=" + secretMark},
		{"env passwd", "DB_PASSWD=" + pw, "DB_PASSWD=" + secretMark},
		{"pgpassword", "PGPASSWORD=" + pw, "PGPASSWORD=" + secretMark},
		{"export", "export SMTP_PASS=" + pw, "export SMTP_PASS=" + secretMark},
		{"inline", "run REDIS_PASS=" + pw + " npm start", "run REDIS_PASS=" + secretMark + " npm start"},
		{"letters only", "LDAP_PASS=" + j("correct", "horse"), "LDAP_PASS=" + secretMark},
		{"json", `{"db_pass": "` + pw + `"}`, `{"db_pass": "` + secretMark + `"}`},
		{"yaml", "  smtp_pass: " + pw, "  smtp_pass: " + secretMark},
		{"dotted", "spring.mail.pass=" + pw, "spring.mail.pass=" + secretMark},
		{"camel case", `dbPass = "` + pw + `"`, `dbPass = "` + secretMark + `"`},
		{"camel case pwd", "userPwd: " + j("hunter", "hunter"), "userPwd: " + secretMark},
		{"capitals", "DBPass: " + pw, "DBPass: " + secretMark},
		{"double underscore", "SMTP__PASS=" + pw, "SMTP__PASS=" + secretMark},
		{"double underscores", "Email__Smtp__Pass=" + pw, "Email__Smtp__Pass=" + secretMark},
		{"double underscore pwd", "Db__Pwd=" + pw, "Db__Pwd=" + secretMark},
		// A quoted value, spaces and all.
		{"quoted with spaces", `SMTP_PASS="` + j("qwer tyui", " opas dfgh") + `"`, `SMTP_PASS="` + secretMark + `"`},
		{"quoted with a symbol", `SMTP_PASS="` + j("Tr0ub4dor", " &3") + `"`, `SMTP_PASS="` + secretMark + `"`},
		{"export quoted", `export DB_PASS="` + j("elephant horse", " battery staple") + `"`, `export DB_PASS="` + secretMark + `"`},
		{"single-quoted pwd", "MYSQL_PWD='" + j("my secret", " pass phrase") + "'", "MYSQL_PWD='" + secretMark + "'"},
		{"yaml quoted", `  DB_PASS: "` + j("Xk9m", " P2vLq7") + `"`, `  DB_PASS: "` + secretMark + `"`},
		{"json quoted", `{"db_pass": "` + j("Xk9m", " P2vLq7!") + `"}`, `{"db_pass": "` + secretMark + `"}`},
		// A quoted value that runs on into the next key is no one value.
		{"backslash before the next key", "export DB_PASS='" + j("Xk9m", "P2vLq7") + "\\' SMTP_PASS='" + pw + "'", "export DB_PASS='" + secretMark + "' SMTP_PASS='" + secretMark + "'"},
		{"backslash before the next key's spaced value", "export DB_PASS='" + j("Xk9m", "P2vLq7") + "\\' SMTP_PASS='" + pw + " more'", "export DB_PASS='" + secretMark + "' SMTP_PASS='" + secretMark + "'"},
		{"open quote before the next key", `DB_PASS="` + j("Xk9m", "P2vLq7") + ` SMTP_PASS="` + pw + ` more"`, `DB_PASS="` + secretMark + ` SMTP_PASS="` + secretMark + ` more"`},
		{"next key glued after a backslash", "x DB_PASS= '" + j("Xk9m", "P2vLq7") + "\\'smtp_pass:'" + pw + "'", "x DB_PASS= '" + secretMark + "'smtp_pass:'" + secretMark + "'"},
		{"password key after a backslash", "export DB_PASSWORD='" + j("Xk9m", "P2vLq7") + "\\' SMTP_PASSWORD='" + pw + "'", "export DB_PASSWORD='" + secretMark + "'" + secretMark + "'"},
		{"escaped quote in a code string", "const DB_PASS = 'it\\'s a long secret'", "const DB_PASS = '" + secretMark + "'"},
		// pass or pwd alone.
		{"connection string", "Server=db;Uid=sa;Pwd=" + pw + ";", "Server=db;Uid=sa;Pwd=" + secretMark + ";"},
		{"url query", "https://x.example/login?user=bob&pass=" + pw, "https://x.example/login?user=bob&pass=" + secretMark},
		{"second pair", "?user=bob&pass=x1&pwd=" + pw, "?user=bob&pass=x1&pwd=" + secretMark},
		{"long option", "openvpn --pass=" + j("correct", "horse"), "openvpn --pass=" + secretMark},
		{"object literal", "auth: { user: 'me@example.com', pass: '" + pw + "!' }", "auth: { user: 'me@example.com', pass: '" + secretMark + "' }"},
		{"yaml alone", "pass: " + pw, "pass: " + secretMark},
		{"php", `$pwd = "` + pw + `";`, `$pwd = "` + secretMark + `";`},
		// A MySQL or MariaDB client's -p<password>.
		{"mysql", "mysql -u root -p" + pw + " app", "mysql -u root -p" + passwordMark + " app"},
		{"short", "mysql -uroot -p" + j("ro", "ot") + " app", "mysql -uroot -p" + passwordMark + " app"},
		{"digits", "mysqldump -uroot -p" + j("123", "456") + " app > dump.sql", "mysqldump -uroot -p" + passwordMark + " app > dump.sql"},
		{"mariadb", "mariadb-dump -uroot -p" + pw + " app", "mariadb-dump -uroot -p" + passwordMark + " app"},
		{"mariabackup", "mariabackup --backup -uroot -p" + pw, "mariabackup --backup -uroot -p" + passwordMark},
		{"single quotes", "mysql -uroot -p'" + spaced + "' app", "mysql -uroot -p'" + passwordMark + "' app"},
		{"double quotes", `mysql -uroot -p"` + pw + `" app`, `mysql -uroot -p"` + passwordMark + `" app`},
		{"single-quoted dollar", "mysql -uroot -p'$" + pw + "'", "mysql -uroot -p'" + passwordMark + "'"},
		{"in a container", "docker exec -it db mysql -uroot -p" + pw + " -e 'select 1'", "docker exec -it db mysql -uroot -p" + passwordMark + " -e 'select 1'"},
		{"by its path", "/usr/local/bin/mysql -uroot -p" + pw, "/usr/local/bin/mysql -uroot -p" + passwordMark},
		{"substitution", "n=$(mysql -uroot -p" + pw + " -N -e 'select 1')", "n=$(mysql -uroot -p" + passwordMark + " -N -e 'select 1')"},
		{"after pymysql", "pip install pymysql && mysql -uroot -p" + pw + " app", "pip install pymysql && mysql -uroot -p" + passwordMark + " app"},
		{"ends the command", "mysql -uroot -p" + pw + "; mysql -p app", "mysql -uroot -p" + passwordMark + "; mysql -p app"},
		{"continued", "mysql -h db \\\n  -uroot \\\n  -p" + pw + " app", "mysql -h db \\\n  -uroot \\\n  -p" + passwordMark + " app"},
		// curl's user:password.
		{"curl", "curl -u admin:" + pw + " https://x.example/api", "curl -u admin:" + passwordMark + " https://x.example/api"},
		{"curl user", "curl --user admin:" + pw + " https://x.example", "curl --user admin:" + passwordMark + " https://x.example"},
		{"curl user equals", "curl --user=admin:" + pw + " https://x.example", "curl --user=admin:" + passwordMark + " https://x.example"},
		{"curl attached", "curl -uadmin:" + pw + " https://x.example", "curl -uadmin:" + passwordMark + " https://x.example"},
		{"curl cluster", "curl -sSu admin:" + pw + " https://x.example", "curl -sSu admin:" + passwordMark + " https://x.example"},
		{"curl quoted", "curl -u 'admin:" + spaced + "' https://x.example", "curl -u 'admin:" + passwordMark + "' https://x.example"},
		{"curl proxy", "curl -U proxy:" + pw + " https://x.example", "curl -U proxy:" + passwordMark + " https://x.example"},
		{"curl proxy user", "curl --proxy-user proxy:" + pw + " https://x.example", "curl --proxy-user proxy:" + passwordMark + " https://x.example"},
		{"curl after a query", `curl "https://x.example/?a=1&b=2" -u admin:` + pw, `curl "https://x.example/?a=1&b=2" -u admin:` + passwordMark},
		{"curl colon in password", "curl -u admin:" + j("ab:cd", "1234") + " https://x.example", "curl -u admin:" + passwordMark + " https://x.example"},
		{"curl continued", "curl -X POST https://x.example/v1 \\\n  -u admin:" + pw + " \\\n  -d '{}'", "curl -X POST https://x.example/v1 \\\n  -u admin:" + passwordMark + " \\\n  -d '{}'"},
		{"curl crlf", "curl -X POST https://x.example/v1 \\\r\n  -u admin:" + pw, "curl -X POST https://x.example/v1 \\\r\n  -u admin:" + passwordMark},
		{"after libcurl", "libcurl is fine, curl -u admin:" + pw + " fails", "libcurl is fine, curl -u admin:" + passwordMark + " fails"},
		{"curl quoted password", "curl -u admin:'" + spaced + "' https://x.example", "curl -u admin:'" + passwordMark + "' https://x.example"},
		{"curl quoted symbols", "curl -u admin:'" + j("Xk9m&P2v", "(Lq7") + "' https://x.example", "curl -u admin:'" + passwordMark + "' https://x.example"},
		{"curl quoted bracket", `curl -u admin:"` + j("Xk9m)P2v", "Lq7") + `" https://x.example`, `curl -u admin:"` + passwordMark + `" https://x.example`},
		{"mysql partly quoted", "mysql -pab'" + j("c ", "d") + "' app", "mysql -p" + passwordMark + " app"},
		// Every password option of a command, the first too, and only its own:
		// a command ends at && or a '&' that sends it to the background.
		{"curl twice", "curl -u admin:" + pw + " https://x.example/a && curl -u admin:" + pw2 + " https://x.example/b", "curl -u admin:" + passwordMark + " https://x.example/a && curl -u admin:" + passwordMark + " https://x.example/b"},
		{"curl proxy and user", "curl -x http://proxy.example:3128 -U proxyuser:" + pw2 + " -u apiuser:" + pw + " https://x.example", "curl -x http://proxy.example:3128 -U proxyuser:" + passwordMark + " -u apiuser:" + passwordMark + " https://x.example"},
		{"curl before docker", "curl -u admin:" + pw + " https://x.example/a && docker run -u 1000:1000 img", "curl -u admin:" + passwordMark + " https://x.example/a && docker run -u 1000:1000 img"},
		{"mysql before cp", "mysql -p" + pw + " app && cp -pr src dst", "mysql -p" + passwordMark + " app && cp -pr src dst"},
		{"mysql before sql", "mysql -p" + pw + " shop -e 'SELECT id, -price AS neg FROM items'", "mysql -p" + passwordMark + " shop -e 'SELECT id, -price AS neg FROM items'"},
		{"curl in a path", "curl -s https://x.example/curl -u admin:" + pw, "curl -s https://x.example/curl -u admin:" + passwordMark},
		{"after which curl", "which curl && curl -u admin:" + pw + " https://x.example", "which curl && curl -u admin:" + passwordMark + " https://x.example"},
		// A quoted string is one word: a ';', '|' or '&' in it ends nothing.
		{"curl after a charset", "curl -H 'Content-Type: application/json; charset=utf-8' -u admin:" + pw + " https://x.example", "curl -H 'Content-Type: application/json; charset=utf-8' -u admin:" + passwordMark + " https://x.example"},
		{"curl after a cookie", `curl -H "Cookie: session=abc; theme=dark" -u admin:` + pw + " https://x.example", `curl -H "Cookie: session=abc; theme=dark" -u admin:` + passwordMark + " https://x.example"},
		{"curl after a user agent", `curl -A "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0)" -u admin:` + pw + " https://x.example", `curl -A "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0)" -u admin:` + passwordMark + " https://x.example"},
		{"curl after a pipe in data", "curl -d 'a=1|b=2' --user admin:" + pw + " https://x.example", "curl -d 'a=1|b=2' --user admin:" + passwordMark + " https://x.example"},
		{"mysql after statements", `mysql -h db -e "SELECT 1; SELECT 2" -uroot -p` + pw + " app", `mysql -h db -e "SELECT 1; SELECT 2" -uroot -p` + passwordMark + " app"},
		{"mysql after quoted sql", "mysql -e 'select 1' -p" + pw + " db", "mysql -e 'select 1' -p" + passwordMark + " db"},
		{"curl after an apostrophe", "curl -d it's -u admin:" + pw + " https://x.example", "curl -d it's -u admin:" + passwordMark + " https://x.example"},
		// A redirection's '&' ends nothing either.
		{"curl after 2>&1", "curl -o out.json 2>&1 -u admin:" + pw + " https://x.example", "curl -o out.json 2>&1 -u admin:" + passwordMark + " https://x.example"},
		{"mysql after &>", "mysql &>mysql.log -uroot -p" + pw + " app", "mysql &>mysql.log -uroot -p" + passwordMark + " app"},
		{"mysql after >&2", "mysql -uroot >&2 -p" + pw + " app", "mysql -uroot >&2 -p" + passwordMark + " app"},
		// Nor does a '&' inside a word, as in an unquoted URL query.
		{"curl after an unquoted query", "curl https://x.example/?a=1&b=2 -u admin:" + pw, "curl https://x.example/?a=1&b=2 -u admin:" + passwordMark},
		{"curl after an unquoted api query", "curl https://x.example/api?user=a&key=b -u admin:" + pw, "curl https://x.example/api?user=a&key=b -u admin:" + passwordMark},
		{"curl post after an unquoted query", "curl -X POST https://x.example/?a=1&b=2 --user admin:" + pw + " -d x", "curl -X POST https://x.example/?a=1&b=2 --user admin:" + passwordMark + " -d x"},
		{"curl password with an ampersand", "curl -u admin:" + j("pa&ss", "W0rd9") + " https://x.example", "curl -u admin:" + passwordMark + " https://x.example"},
		// A backtick ends a command, as inline code in Markdown.
		{"curl in inline code", "run `curl -u admin:" + pw + " https://x.example` first", "run `curl -u admin:" + passwordMark + " https://x.example` first"},
		{"curl value on the next line", "curl https://x.example \\\n  -u \\\n  admin:" + pw, "curl https://x.example \\\n  -u \\\n  admin:" + passwordMark},
		// sshpass, registry logins, redis-cli and openssl.
		{"sshpass", "sshpass -p " + pw + " ssh deploy@host", "sshpass -p " + passwordMark + " ssh deploy@host"},
		{"sshpass attached", "sshpass -p" + pw + " scp f host:", "sshpass -p" + passwordMark + " scp f host:"},
		{"sshpass quoted", "sshpass -p '" + spaced + "' ssh host", "sshpass -p '" + passwordMark + "' ssh host"},
		{"sshpass after its options", "sshpass -v -d 3 -p " + pw + " ssh -p 2222 host", "sshpass -v -d 3 -p " + passwordMark + " ssh -p 2222 host"},
		{"sshpass after a spaced prompt", "sshpass -P 'Pass word:' -p " + pw + " ssh host", "sshpass -P 'Pass word:' -p " + passwordMark + " ssh host"},
		{"docker login", "docker login -u me -p " + pw + " registry.example.com", "docker login -u me -p " + passwordMark + " registry.example.com"},
		{"docker login password", "docker login --username me --password " + pw + " registry.example.com", "docker login --username me --password " + passwordMark + " registry.example.com"},
		{"docker login equals", "docker login --password=" + pw, "docker login --password=" + secretMark},
		{"docker config", "docker --config /tmp/d login -u me -p " + pw, "docker --config /tmp/d login -u me -p " + passwordMark},
		{"podman login", "podman login -u me -p " + pw + " quay.example.com", "podman login -u me -p " + passwordMark + " quay.example.com"},
		{"redis-cli", "redis-cli -h cache -p 6379 -a " + pw + " ping", "redis-cli -h cache -p 6379 -a " + passwordMark + " ping"},
		{"redis-cli pass", "redis-cli --user app --pass " + pw + " info", "redis-cli --user app --pass " + passwordMark + " info"},
		{"openssl pass", "openssl enc -aes-256-cbc -pbkdf2 -pass pass:" + pw + " -in a -out b", "openssl enc -aes-256-cbc -pbkdf2 -pass pass:" + secretMark + " -in a -out b"},
		{"openssl letters", "openssl enc -aes-256-cbc -pass pass:" + j("correct", "horse") + " -in a", "openssl enc -aes-256-cbc -pass pass:" + passwordMark + " -in a"},
		{"openssl passin quoted", "openssl pkcs12 -in c.p12 -passin 'pass:" + spaced + "' -nodes", "openssl pkcs12 -in c.p12 -passin 'pass:" + passwordMark + "' -nodes"},
		{"openssl passout", "openssl genrsa -aes256 -passout pass:" + j("correct", "horse") + " 2048", "openssl genrsa -aes256 -passout pass:" + passwordMark + " 2048"},
		// A command inside another of the same name: in quotes, in $(...),
		// past the first's options or after a '&' inside a word.
		{"sshpass in quotes", "sshpass -p " + pw + " ssh jump 'sshpass -p " + pw2 + " ssh inner'", "sshpass -p " + passwordMark + " ssh jump 'sshpass -p " + passwordMark + " ssh inner'"},
		{"sshpass in double quotes", "sshpass -f pw.txt ssh jump \"sshpass -p " + pw2 + " ssh inner uptime\"", "sshpass -f pw.txt ssh jump \"sshpass -p " + passwordMark + " ssh inner uptime\""},
		{"sshpass past the first's options", "sshpass -e ssh jump sshpass -p " + pw2 + " ssh inner", "sshpass -e ssh jump sshpass -p " + passwordMark + " ssh inner"},
		{"sshpass in a substitution", "sshpass -e ssh host $(sshpass -p " + pw2 + " ssh b cat x)", "sshpass -e ssh host $(sshpass -p " + passwordMark + " ssh b cat x)"},
		{"sshpass after a joined &", "sshpass -p " + pw + " ssh a&sshpass -p " + pw2 + " ssh b", "sshpass -p " + passwordMark + " ssh a&sshpass -p " + passwordMark + " ssh b"},
		{"sshpass from a file before a joined &", "sshpass -f pwfile ssh host&sshpass -p " + pw2 + " ssh b", "sshpass -f pwfile ssh host&sshpass -p " + passwordMark + " ssh b"},
		{"docker login in quotes", "docker run --rm img sh -c 'docker login -u me -p " + pw2 + " r.example'", "docker run --rm img sh -c 'docker login -u me -p " + passwordMark + " r.example'"},
		{"curl in quotes", "curl -d \"$(curl -u admin:" + pw2 + " https://token.example)\" https://x.example", "curl -d \"$(curl -u admin:" + passwordMark + " https://token.example)\" https://x.example"},
		{"mysql in quotes in quotes", "mysql -e \"$(sh -c 'mysql -uroot -p" + pw2 + " -N app')\" app", "mysql -e \"$(sh -c 'mysql -uroot -p" + passwordMark + " -N app')\" app"},
		{"redis-cli in quotes", "redis-cli -h a eval \"$(redis-cli -a " + pw2 + " get script)\" 0", "redis-cli -h a eval \"$(redis-cli -a " + passwordMark + " get script)\" 0"},
		// A command inside quotes is read within them, and the commands
		// after the quotes as their own.
		{"quoted command then curl", "ssh host 'curl -s localhost/health' && curl -u 'admin:" + pw + "' https://api.example.com", "ssh host 'curl -s localhost/health' && curl -u 'admin:" + passwordMark + "' https://api.example.com"},
		{"quoted sh -c then curl", "kubectl exec web -- sh -c 'curl -s localhost:8080/health' && curl -u 'admin:" + pw + "' https://api.example.com", "kubectl exec web -- sh -c 'curl -s localhost:8080/health' && curl -u 'admin:" + passwordMark + "' https://api.example.com"},
		{"double-quoted command then curl", `ssh host "curl -s localhost/health" && curl -u "admin:` + pw + `" https://x.example/api`, `ssh host "curl -s localhost/health" && curl -u "admin:` + passwordMark + `" https://x.example/api`},
		{"quoted mysql then mysql", `ssh db 'mysql -e "select 1"' && mysql -uroot -p'` + pw + `' app`, `ssh db 'mysql -e "select 1"' && mysql -uroot -p'` + passwordMark + `' app`},
		{"quoted redis-cli then redis-cli", "ssh host 'redis-cli ping' && redis-cli -a '" + pw + "' ping", "ssh host 'redis-cli ping' && redis-cli -a '" + passwordMark + "' ping"},
		{"quoted docker then login", "ssh host 'docker ps' && docker login -u me -p '" + pw + "' r.example", "ssh host 'docker ps' && docker login -u me -p '" + passwordMark + "' r.example"},
		{"quoted sshpass then sshpass", "ssh host 'sshpass -V' && sshpass -p '" + pw + "' ssh other", "ssh host 'sshpass -V' && sshpass -p '" + passwordMark + "' ssh other"},
		{"quoted command in prose", "I ran 'curl -I https://x.example' first and it worked, then curl -u 'admin:" + pw + "' https://x.example failed", "I ran 'curl -I https://x.example' first and it worked, then curl -u 'admin:" + passwordMark + "' https://x.example failed"},
		{"quoted command before &&", "sh -c 'curl -u admin:" + pw + "' && echo 'done'", "sh -c 'curl -u admin:" + passwordMark + "' && echo 'done'"},
		{"quoted command before a pipe", "sh -c 'curl -u admin:" + pw + "' | jq '.x'", "sh -c 'curl -u admin:" + passwordMark + "' | jq '.x'"},
		{"quoted command before a redirection", "docker exec db sh -c 'mysql -uroot -p" + pw + "' < dump.sql && echo 'restored'", "docker exec db sh -c 'mysql -uroot -p" + passwordMark + "' < dump.sql && echo 'restored'"},
		{"glued quotes in a quoted curl", `sh -c "curl -u "admin:` + pw + `" https://x.example"`, `sh -c "curl -u "admin:` + passwordMark + `" https://x.example"`},
		{"glued quotes in a quoted mysql", `ssh host "mysql -uroot -p"` + pw + `" app"`, `ssh host "mysql -uroot -p"` + passwordMark + `" app"`},
		// Quotes inside "$(...)" belong to the substitution.
		{"curl in a quoted substitution", `curl -s "https://api.example/items?id=$(curl -s -u "admin:` + pw + `" https://auth.example/id)"`, `curl -s "https://api.example/items?id=$(curl -s -u "admin:` + passwordMark + `" https://auth.example/id)"`},
		{"curl data from a quoted substitution", `curl -sS -d "$(curl -su "admin:` + pw + `" https://x.example/config)" https://y.example/import`, `curl -sS -d "$(curl -su "admin:` + passwordMark + `" https://x.example/config)" https://y.example/import`},
		{"curl header from a quoted substitution", `curl -H "X-Token: $(curl -s -u "admin:` + pw + `" https://auth.example/t)" https://api.example/x`, `curl -H "X-Token: $(curl -s -u "admin:` + passwordMark + `" https://auth.example/t)" https://api.example/x`},
		{"redis-cli in a quoted substitution", `redis-cli -a "$(redis-cli -a "` + pw + `" get pw)" ping`, `redis-cli -a "$(redis-cli -a "` + passwordMark + `" get pw)" ping`},
		{"sshpass in a quoted substitution", `sshpass -e ssh host "$(sshpass -p "` + pw + `" ssh b cat x)"`, `sshpass -e ssh host "$(sshpass -p "` + passwordMark + `" ssh b cat x)"`},
		{"docker login in a quoted substitution", `docker run img sh -c "$(docker login -u me -p "` + pw + `" r.example)"`, `docker run img sh -c "$(docker login -u me -p "` + passwordMark + `" r.example)"`},
		// A bearer token a command fetches: the baseline rules take the
		// substitution's start as the token, and the command is read.
		{"bearer from curl", `curl -H "Authorization: Bearer $(curl -s -u client:` + pw + ` https://auth.example/token | jq -r .access_token)" https://api.example/x`, `curl -H "Authorization: Bearer [REDACTED:auth_header] -s -u client:` + passwordMark + ` https://auth.example/token | jq -r .access_token)" https://api.example/x`},
		{"bearer from curl, quoted", `curl -H "Authorization: Bearer $(curl -s -u "client:` + pw + `" https://auth.example/token)" https://api.example/x`, `curl -H "Authorization: Bearer [REDACTED:auth_header] -s -u "client:` + passwordMark + `" https://auth.example/token)" https://api.example/x`},
		// Operators inside a bracket the command opened end no command.
		{"mysql after && in a substitution", "mysql -h $(cd infra && terraform output -raw db_host) -uroot -p" + pw + " app", "mysql -h $(cd infra && terraform output -raw db_host) -uroot -p" + passwordMark + " app"},
		{"mysql after ; in a substitution", "mysql -h $(cd infra; terraform output -raw db_host) -uroot -p" + pw + " app", "mysql -h $(cd infra; terraform output -raw db_host) -uroot -p" + passwordMark + " app"},
		{"mysql after | in a substitution", "mysql -h $(terraform output -raw db_host | tr -d x) -uroot -p" + pw + " app", "mysql -h $(terraform output -raw db_host | tr -d x) -uroot -p" + passwordMark + " app"},
		{"curl after && in a substitution", "curl -d $(cd /x && cat body.json) -u admin:" + pw + " https://x.example", "curl -d $(cd /x && cat body.json) -u admin:" + passwordMark + " https://x.example"},
		// Quotes a reading that pairs every quote pairs wrongly.
		{"curl after ANSI-C quotes holding a ;", `curl -d $'a\'b; c' -u admin:` + pw + ` https://x.example`, `curl -d $'a\'b; c' -u admin:` + passwordMark + ` https://x.example`},
		{"curl after ANSI-C quotes", `curl -d $'it\'s' -u admin:` + pw + ` -H 'X: y' https://x.example`, `curl -d $'it\'s' -u admin:` + passwordMark + ` -H 'X: y' https://x.example`},
		{"curl after an apostrophe in a URL", "curl https://api.example.com/users/o'neil -u admin:" + pw + " -H 'Accept: application/json'", "curl https://api.example.com/users/o'neil -u admin:" + passwordMark + " -H 'Accept: application/json'"},
		{"mysql after an apostrophe in a name", "mysql -u o'brien -p" + pw + " app -e 'show tables'", "mysql -u o'brien -p" + passwordMark + " app -e 'show tables'"},
		{"redis-cli after an apostrophe", "redis-cli -h o'host -a " + pw + " get 'k'", "redis-cli -h o'host -a " + passwordMark + " get 'k'"},
		{"curl after an apostrophe, quotes later", "curl -d it's -u admin:" + pw + " https://x.example -H 'Accept: application/json'", "curl -d it's -u admin:" + passwordMark + " https://x.example -H 'Accept: application/json'"},
		// A backtick substitution, a word of its own or inside one.
		{"mysql after a backtick substitution", "mysql -h `cat host.txt` -uroot -p" + pw + " app", "mysql -h `cat host.txt` -uroot -p" + passwordMark + " app"},
		{"curl after a backtick in a word", "curl -H X-Date:`date` -u admin:" + pw + " https://x.example", "curl -H X-Date:`date` -u admin:" + passwordMark + " https://x.example"},
		{"curl in inline code before more", "run `curl -u admin:" + pw + " https://x.example` first, then `ls -la` and so on", "run `curl -u admin:" + passwordMark + " https://x.example` first, then `ls -la` and so on"},
		// The option where a value was due is judged as the option.
		{"curl -u twice", "curl -u -u admin:" + pw + " https://x.example", "curl -u -u admin:" + passwordMark + " https://x.example"},
		{"sshpass -p after -d", "sshpass -d -p " + pw + " ssh host", "sshpass -d -p " + passwordMark + " ssh host"},
		// A password in prose.
		{"prose", "my password is " + j("Hunter", "2!x"), "my password is " + passwordMark},
		{"prose symbols", "the db password is " + j("tr0ub4", "dor&3"), "the db password is " + passwordMark},
		{"was", "the password was " + j("p4ss", "w0rd") + ".", "the password was " + passwordMark + "."},
		{"pass", "the pass is " + j("Summer", "2024"), "the pass is " + passwordMark},
		{"pw", "pw was " + j("abc", "123") + ", then", "pw was " + passwordMark + ", then"},
		{"tab", "password\tis " + j("abc", "123x"), "password\tis " + passwordMark},
		{"passwd", "the passwd is " + j("Tr0ub4", "dor"), "the passwd is " + passwordMark},
		{"prose in capitals", "PASSWORD IS " + j("Summer", "2024!"), "PASSWORD IS " + passwordMark + "!"},
		{"colon", "Password is: " + j("Hunter", "2!x"), "Password is: " + passwordMark},
		{"backticks", "the password is `" + j("hunt", "er2") + "`", "the password is `" + passwordMark + "`"},
		{"quoted with a space", `my password is "` + j("Tr0ub4", " dor&3") + `".`, `my password is "` + passwordMark + `".`},
		{"in brackets", "(the password is " + j("Hunter", "2!x") + ")", "(the password is " + passwordMark + ")"},
		{"random letters", "the password is " + j("qWeRtY", "uIoPaS"), "the password is " + passwordMark},
		// A number and a unit joined to a word are part of a password, not a
		// measurement, and so are digits and one letter.
		{"prose joined to seconds", "the password is " + j("Hunter", "_42s"), "the password is " + passwordMark},
		{"prose joined to a time", "the password is " + j("Hunter", "_42ms"), "the password is " + passwordMark},
		{"key joined to a size", "pass: " + j("Dragon", "-512mb"), "pass: " + secretMark},
		{"prose joined to a factor", "password is " + j("Dragon", "_99x"), "password is " + passwordMark},
		{"prose joined to a year", "my password is " + j("Summer", "_2024s"), "my password is " + passwordMark},
		{"prose joined to an hour", "password was " + j("Welcome", "-1h"), "password was " + passwordMark},
		{"pw joined to bytes", "my pw is " + j("Monkey", ".123b"), "my pw is " + passwordMark},
		{"prose digits and a letter", "the password is " + j("1234", "567s"), "the password is " + passwordMark},
		{"dotted pass joined to a year", "db.pass: " + j("Summer", "_2024s"), "db.pass: " + secretMark},
		{"yaml alone joined to a factor", "pass: " + j("Dragon", "_99x"), "pass: " + secretMark},
		{"json alone joined to an hour", `"pass": "` + j("Welcome", "-1h") + `"`, `"pass": "` + secretMark + `"`},
		{"stage key joined to a factor", "render_pass = " + j("Spring", "_2025x"), "render_pass = " + secretMark},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, n := Redact(c.in)
			if out != c.want || n == 0 {
				t.Fatalf("Redact(%q)\n got %q (%d)\nwant %q", c.in, out, n, c.want)
			}
			if twice, n := Redact(out); twice != out || n != 0 {
				t.Fatalf("not idempotent: %q", twice)
			}
		})
	}
}

// Provider tokens with a fixed prefix are replaced whole, under their own
// kind, in text, by RedactKnown and in identifiers.
func TestTokenPrefixes(t *testing.T) {
	isolate(t)
	for _, c := range []struct{ kind, token string }{
		{"huggingface_token", j("hf", "_", "AbCdEfGhIjKlMnOpQrStUvWxYz01234567")},
		{"npm_token", j("np", "m_", "AbCdEfGhIjKlMnOpQrStUvWxYz0123456789")},
		{"pypi_token", j("pypi-", "AgEIcHlwaS5vcmc", strings.Repeat("Ab3dE", 12))},
		{"vault_token", j("hv", "s.", strings.Repeat("CAESIJlU9JMYEhOPYv4KMr7Fx", 4))},
		{"vault_token", j("hv", "b.", strings.Repeat("AAAAAQKk9GGVRwPBNnxuPDsl2", 6))},
		{"digitalocean_token", j("do", "p_v1_", strings.Repeat("a1b2c3d4", 8))},
		{"digitalocean_token", j("do", "o_v1_", strings.Repeat("e5f6a7b8", 8))},
		{"digitalocean_token", j("do", "r_v1_", strings.Repeat("c9d0e1f2", 8))},
		{"sendgrid_key", j("S", "G.", "AbCdEfGhIjKlMnOpQrStUv", ".", "AbCdEfGhIjKlMnOpQrStUvWxYz0123456789-_AbCdE")},
		{"shopify_token", j("shp", "at_", strings.Repeat("a1b2c3d4", 4))},
		{"shopify_token", j("shp", "ss_", strings.Repeat("0f9e8d7c", 4))},
		{"docker_token", j("dckr", "_pat_", "Ab3dEf6hIj9lMn2pQr5tUv8xYz0")},
	} {
		in := "use " + c.token + " now"
		if out, n := Redact(in); out != "use [REDACTED:"+c.kind+"] now" || n != 1 {
			t.Errorf("Redact(%q) = %q, %d", in, out, n)
		}
		if out, _ := RedactKnown(c.token); out != "[REDACTED:"+c.kind+"]" {
			t.Errorf("RedactKnown(%q) = %q", c.token, out)
		}
		if out, n := Identifier("fix/"+c.token, 1024); n == 0 || !hashedID.MatchString(out) || strings.Contains(out, c.token[4:16]) {
			t.Errorf("Identifier(fix/%q) = %q, %d", c.token, out, n)
		}
	}
}

// Text the new rules must leave alone: other commands' -p and -u options,
// a -p that asks for the password, variables, words that hold pass or pwd,
// test results, code, and prose that says what a password is like.
func TestPasswordShapesKept(t *testing.T) {
	isolate(t)
	for _, in := range []string{
		// Other commands' options, and options that ask for the password.
		"mkdir -p /tmp/x/y",
		"git add -p",
		"cp -p a b",
		"ssh -p 22 host",
		"ssh -p2222 host && mysql -uroot -p",
		"ssh mysql-host -p2222",
		"docker run -p 8080:80 nginx",
		"docker login -u me --password-stdin registry.example.com < token.txt",
		"docker login registry.example.com && docker run -p 8080:80 nginx",
		"docker login -u me -p \"$REGISTRY_PASSWORD\" registry.example.com",
		"podman run -p 5432:5432 postgres",
		"sshpass -f ~/.ssh/pw ssh -p 2222 host",
		"sshpass -e ssh host $(sshpass -f pwfile) -p 2222 jump",
		"echo $(mysql -p app) && cp -pr a b",
		"sshpass -f pw.txt ssh jump 'sshpass -f other ssh -p 2222 inner'",
		"sshpass -e ssh -p 22 host",
		"sshpass -P 'Password:' ssh -p 22 host",
		"redis-cli -h cache -p 6379 ping",
		"redis-cli --askpass -h cache",
		"redis-cli -a \"$REDIS_PASSWORD\" ping",
		"openssl enc -aes-256-cbc -pass env:ENC_PASS -in a",
		"openssl pkcs12 -in c.p12 -passin file:pw.txt",
		"openssl rsa -in k.pem -passin stdin",
		"openssl req -new -key k.pem -passin fd:3",
		"mysql -u root -p mydb",
		"mysql -P 3306 -u root -p",
		"mysql -uroot -p",
		`mysql -uroot -p"$MYSQL_PWD" app`,
		"mysql -uroot -p$MYSQL_PWD app",
		"mysql -uroot -p${DB_PASS} app",
		"mysql -uroot -p<password> app",
		"mysql_config --libs -pthread",
		"cc -o app main.c -lmysql -pthread",
		"pymysql -p connects",
		"curl -u admin https://x.example",
		"curl -u admin: https://x.example",
		`curl -u "$USER:$PASS" https://x.example`,
		"curl -u admin:$(cat pw.txt) https://x.example",
		`curl --user-agent "x:y" https://x.example`,
		`curl -H "X-Auth-User: bob:secret12" https://x.example`,
		"sort -u names.txt",
		// A command ends at "&&", "||", '&', ';', '|' or a ')' it didn't open,
		// but not inside quotes, and only its own options are read.
		"mysql -uroot -p app < schema.sql && ssh -p2222 deploy@host",
		"mysql -uroot -p app < schema.sql && cp -pr src dst",
		"mysql -uroot -p app && go test -p4 ./...",
		"mysql -uroot -p app & cp -pr src dst",
		"mysql -uroot -p app &cp -pr src dst",
		"mysql -uroot -p app& cp -pr src dst",
		"mysql -uroot -p app&&cp -pr src dst",
		"curl -s https://x.example/?a=1&b=2 & sudo -u www-data:www-data ls",
		"mysql -uroot -p app || ssh -p2222 host",
		"mysql -p app && cp -pr src dst",
		"mysql -u root -p -e 'SELECT * FROM items ORDER BY -position DESC' shop",
		"curl -fsSL https://get.example.com -o get.sh && docker run --rm -u 1000:1000 node:20 npm ci",
		"curl -sS https://x.example && sudo -u www-data:www-data ls",
		"curl -H 'X-Note: a; b' https://x.example | sudo -u www-data:www-data tee out",
		"docker run $(curl -s https://x.example/env) -u 1000:1000 img",
		"sshpass -v ssh -p 22 host",
		`curl -u admin:"$PASS" https://x.example`,
		`curl -u admin:"$(cat pw.txt)" https://x.example`,
		`curl -u "$USER":"$PASS" https://x.example`,
		// A short option that takes a value ends a cluster: -H's is a header.
		"curl -HUser-Agent:x https://x.example",
		// A backtick ends a command, as inline code in Markdown, and an option's
		// value comes after blanks only, never after an operator.
		"the docs name `sshpass -p`, `docker login -p`, `redis-cli -a` and openssl's `pass:<password>`",
		"use `curl -s https://x.example/env` and `sudo -u www-data:www-data ls` to check",
		"`mysql -uroot -p` asks for it, and `go test -p4 ./...` runs four",
		"the one `docker login` or `podman login` takes after `-p` or `--password` (never `--password-stdin`)",
		"redis-cli -a <password> ping",
		"sshpass -p <password> ssh host",
		"docker login -u me -p <token> registry.example.com",
		// Prose about a password.
		"the password is in the vault",
		"a pass is required",
		"the password is case-sensitive",
		"the password is required",
		"the password was reset",
		"the password is wrong!",
		"the password is 12 characters long",
		"the password is 8+ chars",
		"the password is sha256-hashed",
		"the password is base64-encoded",
		"password is base64",
		"the password is SHA-256 hashed",
		"the password is 1Password-managed",
		"the password is ~/.pgpass",
		"password is getpass()",
		`password is "correct horse battery staple"`,
		"the password is $DB_PASS",
		"password is <your password>",
		"the pass was e-mailed",
		"the boarding pass is ready",
		"pw is wrong",
		"self.password is None",
		"if password is not None:",
		"password: is set",
		"password is [REDACTED:password]",
		"the password is CorrectHorseBattery",
		// Measurements after a render, compiler or test pass.
		"the shadow pass was 16.6ms",
		"the first pass was 120ms, the second pass was 85.5ms",
		"each pass is 1.25GB",
		"the depth pre-pass is 2.35ms on M2",
		"the bloom pass was 0.85ms",
		"the pass took 1200ms and the pass was 1500ms",
		"the pass was 100MB/s",
		"the pass was 12.75x",
		// Words that hold pass or pwd, and keys whose value is no password.
		"PWD=/Users/me/projects",
		"OLDPWD=/tmp",
		"SAVED_PWD=/Users/me/projects/app",
		"export START_PWD=~/projects/app",
		"export PWD=$HOME/projects/app",
		`cmd.Env = append(cmd.Env, "PWD="+cmd.Dir)`,
		`t.Logf("with PWD=%s\nsubprocess reported %s", dir, got)`,
		"pwd = os.getcwd()",
		"pwd := os.Getwd()",
		"bypass=trueish-value",
		"compass: north-north-west",
		"PASS_THROUGH=1",
		"DB_PASS_FILE=/run/secrets/db",
		"pass: true",
		"onPass: handlePassEvent,",
		"renderPass: renderPassDescriptor",
		"render_pass: deferred_lighting",
		"const pass = req.body.pass;",
		"--- PASS: TestParseV2 (0.00s)",
		"PASS: TestOAuth2Flow",
		"DB_PASS=<your db password>",
		"DB_PASS=${DB_PASSWORD}",
		`DB_PASS="${DB_PASSWORD}"`,
		`DB_PASS="<your db password>"`,
		`DB_PASS="$(cat /run/db_pass)"`,
		`db_pass: "hello world"`,
		`PWD="/Users/me/my projects"`,
		// Code between two strings, the first ending with the key.
		`fmt.Println("DB_PASS=" + dbPass + " for staging")`,
		`if !strings.HasPrefix(out, "update: 1 pass: ") || err != nil || !strings.Contains(log, "x") {`,
		// Names that start like a provider token.
		"npm_config_registry=https://registry.npmjs.org/",
		"from huggingface_hub import hf_hub_download",
		"hf_" + strings.Repeat("abcd", 9),
		"dependabot/npm_and_yarn/lodash-4.17.21",
		"msg.SGTIN.Value",
		"shapes/region.shp",
	} {
		if out, n := Redact(in); out != in || n != 0 {
			t.Errorf("Redact(%q) = %q, %d; want unchanged", in, out, n)
		}
	}
}

// Branch names and project keys made of words keep their names: none of the
// new rules reads them as a password or a token.
func TestPasswordShapesKeepIdentifiers(t *testing.T) {
	isolate(t)
	for _, id := range []string{
		"feature/db-pass-rotation",
		"fix/mysql-p-flag",
		"fix/curl-u-option",
		"fix/password-is-required",
		"hf_transformers_upgrade",
		"npm_publish_workflow",
		"dependabot/npm_and_yarn/lodash-4.17.21",
		"local-hf_hub_experiments-a1b2c3",
		"local-render_pass-e82b2c",
		"github.com__acme__shopify-app",
		"feature/hf_" + strings.Repeat("abcd", 9),
	} {
		if out, n := Identifier(id, 1024); out != id || n != 0 {
			t.Errorf("Identifier(%q) = %q, %d; want unchanged", id, out, n)
		}
		if out, n := RedactKnown(id); out != id || n != 0 {
			t.Errorf("RedactKnown(%q) = %q, %d; want unchanged", id, out, n)
		}
	}
}

// Redacting twice changes nothing, for random mixes of the words and
// symbols the new rules read, and every marker a redaction adds is whole.
func TestPasswordShapesIdempotentOnRandomInput(t *testing.T) {
	isolate(t)
	alphabet := []string{
		"mysql ", "mariadb-dump ", "curl ", "pymysql ", " -p", " -u ", "-uadmin:", "--user=", "--user ", " -U ", "admin:", ":",
		"password is ", "pass was ", "pw is ", "is: ", "DB_PASS=", "MYSQL_PWD=", "db_pass: ", "dbPass = ", "pass: ", "pwd=", "&pass=", ";Pwd=", "--pass=",
		"PASS: ", "PWD=/", "'", "\"", "`", "(", ")", ";", "|", "&&", "\\\n", "\n", " ", "\t", "S3cr", "3tPw9", "Ab3!", "x9", "$X", "${X}",
		"<pw>", "[REDACTED:password]", "[REDACTED:", "hf_", "npm_", "Ab3d", "!", ".", ",", "base64", "-hashed", "~/",
		"&", "2>&1", "SMTP__PASS=", " login ", "docker ", "sshpass ", "redis-cli ", " -a ", "openssl ", " -pass ", "pass:",
		j("sk", "_test_", "Ab3dEf6hIj9lMn2pQr5tUv8x"), j("gh", "p_", "Ab3dEf6hIj9lMn2pQr5tUv8xYz0Ab3dEf6hI"),
	}
	seed := uint32(11)
	next := func(n int) int {
		seed = seed*1664525 + 1013904223
		return int(seed>>8) % n
	}
	for i := 0; i < 3000; i++ {
		var b strings.Builder
		for k := next(24); k > 0; k-- {
			b.WriteString(alphabet[next(len(alphabet))])
		}
		once, _ := Redact(b.String())
		if twice, n := Redact(once); twice != once || n != 0 {
			t.Fatalf("not idempotent on %q: %q then %q", b.String(), once, twice)
		}
		in := clean(b.String())
		added := strings.Count(once, marker) - strings.Count(in, marker)
		if whole := len(markerRe.FindAllString(once, -1)) - len(markerRe.FindAllString(in, -1)); added > whole {
			t.Fatalf("a marker cut on %q: %q", b.String(), once)
		}
		id, _ := Identifier(b.String(), 1024)
		if again, n := Identifier(id, 1024); again != id || n != 0 {
			t.Fatalf("Identifier not stable on %q: %q then %q", b.String(), id, again)
		}
	}
}

// A rule's gate only spares it a search that can't match: wherever its
// pattern matches, the gate holds, in the shapes the rules take and in
// random mixes of their words.
func TestPasswordGatesCoverTheirPatterns(t *testing.T) {
	isolate(t)
	var gated []rule
	for _, r := range rules {
		if r.gate != nil {
			gated = append(gated, r)
		}
	}
	if len(gated) == 0 {
		t.Fatal("no rule has a gate")
	}
	words := []string{"pass", "PASS", "Pass", "pwd", "PWD", "pw", "password", "passwd", "PassWord", " ", "\t", "  ", "is", "IS", "was", "Was", ":", "=", "\"", "'",
		"DB_", "db.", "-", "--", "?", "&", ";", "x", "S3cr3tPw9", "Ab3!", "\n", "word", "wd", "by", "OLD"}
	seed := uint32(5)
	next := func(n int) int {
		seed = seed*1664525 + 1013904223
		return int(seed>>8) % n
	}
	matched := 0
	for i := 0; i < 20000; i++ {
		var b strings.Builder
		for k := 1 + next(10); k > 0; k-- {
			b.WriteString(words[next(len(words))])
		}
		text := b.String()
		for _, r := range gated {
			if r.re.MatchString(text) {
				matched++
				if !r.runs(strings.ToLower(text)) {
					t.Fatalf("%s pattern matches %q, but its gate says no", r.kind, text)
				}
			}
		}
	}
	if matched < 100 {
		t.Fatalf("only %d matches, too few to prove anything", matched)
	}
}

// What the new rules replace never holds a newline, so a text cut into
// windows at line ends is redacted as the whole text would be.
func TestPasswordShapesKeepLines(t *testing.T) {
	isolate(t)
	for _, in := range []string{
		"mysql -uroot -p'abc\ndef' app",
		"curl -u 'admin:abc\ndef' https://x.example",
		"my password is \"Tr0ub4\ndor&3\"",
		"DB_PASS=\n" + j("S3cr3t", "Pw9"),
	} {
		out, _ := Redact(in)
		if strings.Count(out, "\n") != strings.Count(in, "\n") {
			t.Errorf("Redact(%q) = %q changes the lines", in, out)
		}
	}
}

// A token that names curl's user is replaced under its own kind, and the
// ':' inside its marker is not the one before a password. No current rule
// cuts a marker in two, as the rule for a URL's credentials did after a
// token that named the URL's user.
func TestPasswordShapesKeepMarkersWhole(t *testing.T) {
	isolate(t)
	stripeKey := j("sk", "_test_", "Ab3dEf6hIj9lMn2pQr5tUv8x")
	ghToken := j("gh", "p_", "Ab3dEf6hIj9lMn2pQr5tUv8xYz0Ab3dEf6hI")
	for _, c := range []struct {
		in, want string
		n        int
	}{
		{"curl https://api.example.com/v1/charges -u " + stripeKey + ": -d amount=2000", "curl https://api.example.com/v1/charges -u [REDACTED:stripe_key]: -d amount=2000", 1},
		{"curl -u " + ghToken + ":x-oauth-basic https://api.example.com/user", "curl -u [REDACTED:github_token]:" + passwordMark + " https://api.example.com/user", 2},
	} {
		if out, n := Redact(c.in); out != c.want || n != c.n {
			t.Errorf("Redact(%q)\n got %q (%d)\nwant %q (%d)", c.in, out, n, c.want, c.n)
		}
	}
	// The current rules alone: the baseline rules cut this marker themselves.
	in := "git clone https://" + ghToken + ":x-oauth-basic@git.example.com/o/r"
	if out, _ := RedactKnown(in); out != "git clone https://[REDACTED:github_token]:x-oauth-basic@git.example.com/o/r" {
		t.Errorf("RedactKnown(%q) = %q", in, out)
	}
}

// The command rules read each command once in each of their two readings,
// so their cost grows with the text, however many command names one long
// line holds: the bytes they hand their patterns, and those they read as
// commands, stay within seven times the text's length. A command inside
// quotes is read again inside them, and the kinds of quote can nest only
// once each: two in the first reading, so a byte is read at most three
// times there, and three in the second, which also reads a backtick
// substitution again, so at most four. Before, each name read its command
// to the end, and one line of names cost the square of its length.
func TestPasswordCommandsReadEachCommandOnce(t *testing.T) {
	isolate(t)
	const size = 32 << 10
	repeat := func(s string) string { return strings.Repeat(s, size/len(s)+1) }
	ran := 0
	for _, r := range rules {
		if r.command == nil {
			continue
		}
		ran++
		for _, name := range r.command.names {
			if name == "maria" {
				name = "mariadb" // what the name starts with, and a whole name
			}
			var spec strings.Builder
			spec.WriteString(`{"paths":{`)
			for spec.Len() < size {
				spec.WriteString(`"/v1/items/1":{"get":{"x-codeSamples":[{"lang":"Shell","source":"` + name + ` -X GET https://api.example.com/v1/items/1 -H 'Accept: application/json'"}]}},`)
			}
			for _, text := range []string{
				repeat(name + " "),
				repeat(name + " login "),
				repeat("'" + name + " -x "),
				repeat(name + ` "`),
				repeat(name + ` \"`),
				repeat(name + " \\\n"),
				repeat(name + " -sS https://x.example/" + name + " -o out && "),
				repeat(name + " https://x.example/?q=" + name + "&a=1 "),
				// Commands inside commands of the same name, each read
				// once, and those in quotes, in two kinds of quotes, read
				// again inside them.
				repeat(name + " -e ssh a "),
				repeat(name + " -f " + name + " "),
				repeat(name + " x&" + name + " "),
				repeat(name + " $(" + name + " -x "),
				repeat(name + " '" + name + " -x "),
				repeat(name + ` -d "$(` + name + ` -x 'sh -c ` + name + ` -x' a)" `),
				repeat(`"` + name + ` '` + name + ` -x '"`),
				repeat(name + " `" + name + ` "` + name + ` '` + name + " -x' x\" x` "),
				repeat("`" + name + " -x` "),
				repeat(name + " -h `"),
				spec.String(),
			} {
				handed, read := 0, 0
				count := func(re *regexp.Regexp, s string) []int {
					handed += len(s)
					return re.FindStringIndex(s)
				}
				commandsRead = func(n int) { read += n }
				commandMatches(text, r.re, r.command, count)
				commandsRead = nil
				if handed > 7*len(text) || read > 7*len(text) {
					t.Errorf("%s: %d bytes handed to the patterns and %d read as commands for a text of %d, starting %q", name, handed, read, len(text), text[:40])
				}
			}
		}
	}
	if ran != 6 {
		t.Fatalf("%d command rules, want 6", ran)
	}
}

// A command rule's two readings can take one password with different
// bounds (see commandMatches): overlapping spans are joined, so what either
// takes is replaced, and spans that only touch stay apart.
func TestMergeOverlapsJoinsSpans(t *testing.T) {
	got := mergeOverlaps([]span{{10, 12}, {3, 9}, {1, 5}, {11, 14}, {14, 16}})
	if want := []span{{1, 9}, {10, 14}, {14, 16}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeOverlaps = %v, want %v", got, want)
	}
}

// design.md's examples of the PASS and PWD keys hold: a key it says loses
// any value of 8 characters or more loses an ordinary one, and a quoted one
// whole, and a key it says loses a value only when it looks like a password
// keeps an ordinary one.
func TestDesignsPassKeyExamples(t *testing.T) {
	isolate(t)
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "design.md"))
	if err != nil {
		t.Fatal(err)
	}
	design := string(data)
	keys := func(after, before string) []string {
		_, rest, ok := strings.Cut(design, after)
		rest, _, ok2 := strings.Cut(rest, before)
		var out []string
		for _, m := range regexp.MustCompile("`([A-Za-z_.]+)`").FindAllStringSubmatch(rest, -1) {
			out = append(out, m[1])
		}
		if !ok || !ok2 || len(out) < 3 {
			t.Fatalf("design.md's examples after %q: %q", after, out)
		}
		return out
	}
	for _, key := range keys("after one or more separators or a change of case, such as ", ", with a quoted value taken whole") {
		for _, value := range []string{"deferred-render", `"qwer tyui opas dfgh"`} {
			if out, _ := Redact(key + "=" + value); strings.Contains(out, "deferred") || strings.Contains(out, "qwer") {
				t.Errorf("design.md says %s loses %s: %q", key, value, out)
			}
		}
	}
	for _, key := range keys("A key ending in `pass` or `Pass`, as in ", ", and `pass` or `pwd` alone anywhere else") {
		if in := key + "=deferred-render"; func() bool { out, _ := Redact(in); return out != in }() {
			t.Errorf("design.md says %s keeps an ordinary value", key)
		}
	}
}
