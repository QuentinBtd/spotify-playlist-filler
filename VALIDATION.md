# Validations réellement exécutées — préparation du 1er octobre, correctifs du 2 octobre 2026

Les compilations et tests Go de préparation ont eu lieu dans `validation/source-checkout/`, copie isolée de `/root/spotify-playlist-filler`. Les tests scripts du correctif ont lieu dans le dossier préparé puis dans une extraction contrôlée du payload. Aucun fichier du dépôt source n'a été écrit.

| Contrôle | Résultat réel | Preuve |
|---|---|---|
| Parsing YAML CI/release/GoReleaser | Succès PyYAML 6.0.3 | Schémas et fichiers du payload |
| JSON Release Please | Valide contre schéma 17.6.0 embarqué par l'action v5 | `validation/sources/rp-schema-pinned.json` |
| JSON schema GoReleaser | Valide contre schéma généré par OSS 2.18.2 | `validation/sources/goreleaser-schema.json` |
| actionlint 1.7.12 | Exit 0, aucune erreur | `validation/actionlint-final.log` |
| GoReleaser `check` | Exit 0, une configuration validée | `validation/goreleaser-check.log` |
| `mise run check` | Tests des cinq packages, vet et format passent | `validation/mise-check-race-build.log` |
| `mise run race` | Les cinq packages passent avec race detector | même log |
| `mise run build` | Exit 0 | même log |
| `go mod tidy` + diff go.mod/go.sum | Exit 0, aucune dérive | `validation/module-tidy.log` |
| GoReleaser snapshot **avec --skip=docker** | Exit 0, six binaires/six archives créés ; aucune publication | `validation/goreleaser-snapshot.log` |
| Checksums des archives | Six SHA-256 recalculés et validés | `validation/archive-verification.log` |
| Smoke test binaire Linux amd64 `--help` | Exit 0, usage affiché, aucun secret/Spotify requis | même log |
| Formats des six binaires | ELF amd64/arm64 statiques, Mach-O amd64/arm64, PE amd64/arm64 | `validation/binary-formats.log` |
| Contenu archives | README, LICENSE, exemple et binaire ; pas de config réelle/.env | `validation/archive-inventory.json` |
| Vérificateur face à corruption | Échec attendu, checksum altéré détecté sur une archive réelle | `validation/verifier-negative.log` |
| Dockerfile : parser BuildKit v0.33.1 | Huit instructions acceptées | `validation/docker-static-fixed.log` |
| `.dockerignore` : matcher Moby v0.6.1 | Treize chemins vérifiés, binaires inclus, secrets/sources exclus | même log |
| Action bundle Release Please épinglé | Config force-tag-creation, draft, updater VERSION et sorties lus | `validation/sources/rp-action-dist.js`, autres sources RP |
| Garde de tag GHCR : tests historiques du 1er octobre | Six cas passaient avant revue ; le cas 404 permissif est invalidé par SEC-01 et remplacé par les tests livrés ci-dessous. Ce log ne valide pas le correctif | `validation/image-guard-unit.log`, ancien `validation/test_image_guard.py` non livré |

Le test initial du denylist a trouvé que réinclure un dossier réincluait aussi ses enfants : correction du `.dockerignore`, puis revalidation réussie. Le log d'échec est conservé sous `validation/docker-static.log` ; seul le fichier corrigé est livré.

La copie mise a d'abord refusé la confiance implicite d'un nouveau répertoire. Les tests ont ensuite utilisé `MISE_TRUSTED_CONFIG_PATHS` limité à la copie contrôlée, avec répertoires data/cache/state/config et caches Go dans le bundle. Aucun `mise trust` sur le dépôt ou changement de configuration utilisateur n'a été fait.

## Correctifs ciblés LOG-01 / SEC-01 — 2 octobre 2026

| Contrôle réexécuté | Résultat réel | Preuve hors payload |
|---|---|---|
| RED 404 ambigu, avant correction | Exit 1 : `HTTPError not raised` (défaut reproduit) | `validation/sec01-red-404.log` |
| GREEN 404 fail-closed | Exit 0, test passe | `validation/sec01-green-404.log` |
| RED bootstrap explicite exact, avant implémentation | Exit 1 : le 404 reste refusé malgré approbation exacte | `validation/sec01-red-bootstrap.log` |
| GREEN bootstrap | Exit 0, cinq tests passent | `validation/sec01-green-bootstrap.log` |
| RED redirection | Exit 1 : cinq requêtes au lieu d'une, transport HTTPS simulé sans réseau | `validation/sec01-red-redirect.log` |
| GREEN redirection | Exit 0, une seule requête sur l'endpoint attendu | `validation/sec01-green-redirect.log` |
| Suite livrée des deux scripts | **22 tests passent** (14 garde, 8 vérificateur), exit 0 | `validation/script-unit-final.log` |
| actionlint 1.7.12, deux workflows corrigés | Exit 0, aucune erreur | `validation/actionlint-blockers-fixed.log` |
| GoReleaser OSS 2.18.2 check, config corrigée | Exit 0, une configuration validée | `validation/goreleaser-check-blockers-fixed.log` |
| Schéma GoReleaser + assertions statiques | Succès ; annotations absentes, quatre labels OCI préservés, deux plateformes, suite branchée dans les deux jobs check ; chaînage same-run conservé | `validation/blockers-static-check.json` |

Commande exacte livrée dans CI et dans le check Release :
`python3 -m unittest discover -s .github/scripts/tests -v`.
Elle a été exécutée localement ; **aucun run CI distant n'est revendiqué**.
Les tests utilisent des fixtures synthétiques explicitement désignées et aucun
vrai token, secret, réponse GHCR ou accès réseau. Le vérificateur existant n'est
pas modifié : ses tests protègent checksums, doublons/traversée, draft/tag, noms
d'assets et plateformes, sans étendre son contrat ni prétendre à la provenance.

LOG-01 : toutes les annotations sont retirées, labels OCI conservés ; la preuve
locale de compatibilité du changement est **statique uniquement**, complétée par
le check/schéma, pas un résultat Docker. CLI absente et socket standard absent
recontrôlés. Aucun daemon installé. Un vrai `release --snapshot --clean` avec
Docker et sans push reste requis sur une machine autorisée équipée.

SEC-01 : défaut fail-closed corrigé et testé ; premier package possible uniquement
par approbation humaine d'un tag exact dans la variable Actions
`SPF_GHCR_BOOTSTRAP_TAG`, vide par défaut. Procédure et suppression immédiate de
la variable documentées dans `INTEGRATION.md`. Cela ne prouve pas l'absence et
n'est pas un bypass pour un package existant. Aucun bootstrap distant réalisé.

Archive mise à jour : **14 fichiers** d'intégration (12 historiques + 2 suites
de tests), sans `validation/`, caches, outils, dist, `.git` ou `__pycache__`.
Le payload rechargé a aussi passé les 22 tests, actionlint et GoReleaser check
(logs `validation/archive-script-unit.log`, `archive-actionlint.log`,
`archive-goreleaser-check.log`). Les six archives réelles existantes ont été
revérifiées, sans nouvelle compilation (`validation/real-archives-blockers-fixed.log`).
Inventaire et SHA-256 exacts dans `validation/delivery.json`. Les preuves et
l'archive historique sont conservées hors payload ; ne pas les intégrer.

## Non exécuté / non prouvé

- Aucune construction ni exécution d'image Docker : CLI `docker` absente ; pas de socket standard détecté. Le parser Dockerfile et matcher ne remplacent **pas** Buildx/daemon.
- Ni OAuth en conteneur ni opérations sur un compte Spotify.
- Aucun run GitHub Actions distant pour ces nouveaux fichiers ; la PR #8 n'a pas été modifiée.
- Ni Release Please contre l'API réelle en mode écriture, ni publication GHCR, ni upload/finalisation de release. Les contrôles de readback distants sont du code préparé, pas des résultats obtenus.
- Exécution native de macOS, Windows ou Linux arm64 non réalisée ; cross-build seulement, Linux amd64 exécuté.
- Le workflow CI complet inclut Docker snapshot : cette étape reste à valider sur le runner équipé après intégration.

## Artefacts réellement produits localement

Sous `validation/source-checkout/dist/`, version **1.1.2-snapshot**, archives :

- `spotify-playlist-filler_1.1.2-snapshot_linux_amd64.tar.gz`
- `spotify-playlist-filler_1.1.2-snapshot_linux_arm64.tar.gz`
- `spotify-playlist-filler_1.1.2-snapshot_darwin_amd64.tar.gz`
- `spotify-playlist-filler_1.1.2-snapshot_darwin_arm64.tar.gz`
- `spotify-playlist-filler_1.1.2-snapshot_windows_amd64.zip`
- `spotify-playlist-filler_1.1.2-snapshot_windows_arm64.zip`
- `checksums.txt`, `metadata.json`, `artifacts.json`.

Ces snapshots ne sont pas des releases et ne sont pas publiés. Pour réexécuter hors Docker : `validation/tools/goreleaser check`, puis `validation/tools/goreleaser release --snapshot --clean --skip=docker` depuis une copie du projet munie du payload et de Go installé. Avec une machine équipée, retirer `--skip=docker` en conservant **--snapshot** pour tester les images sans publication.
