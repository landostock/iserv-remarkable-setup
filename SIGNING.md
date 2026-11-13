# Signierung

RC27 wird als öffentliche Beta bereitgestellt. Die Mac-App ist ad-hoc signiert, aber nicht mit Developer ID signiert oder notarisiert. Die Windows-EXE-Dateien sind nicht mit einem Herausgeberzertifikat signiert. Prüfsummen schützen gegen unbeabsichtigte Dateiänderungen, ersetzen aber keine unabhängige Herausgeberauthentifizierung.

Für eine entsprechend vertrauenswürdige stabile Veröffentlichung sind ein eigener sicher verwahrter Signaturschlüssel, Windows-Codesignierung sowie macOS Developer ID und Notarisierung erforderlich. Zertifikate, Passwörter, private Schlüssel und Signierkonfigurationen mit Geheimnissen gehören nicht in Git.
