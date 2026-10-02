# Livraison CI/CD SPF préparée hors dépôt

## État de cette PR brouillon

Les workflows sont volontairement stockés dans `ci/prepared/ci.yml` et `ci/prepared/release.yml`, **pas** dans `.github/workflows/` : ils sont donc inactifs. Le push des chemins actifs a été refusé par GitHub faute de droit `workflow`. Cette PR dépend du refactoring/mise de la PR #8 et cible sa branche pour isoler le diff CI/CD. Après ajout du droit et fusion de #8, déplacer ces deux fichiers dans `.github/workflows/`, retargeter cette PR vers `main`, relancer les validations, puis sortir du brouillon. Aucune CI, release ou image n’a été publiée par cette préparation. Les chemins `.github/workflows/` décrits ci-dessous désignent les emplacements finaux.

Préparation du **1er octobre 2026**, destinée à être relue puis intégrée après la migration mise. Aucun push, commit, tag, release, image ou réglage distant créé. Le dossier `validation/` contient les preuves, outils temporaires et copie isolée du code ; **ne pas le copier dans le dépôt**. L'archive de payload ne contient que les fichiers d'intégration et cette documentation.

## Fichiers à intégrer

- `.github/workflows/ci.yml` : PR vers main, pushes main et dispatch manuel ; mise `check`, `race`, `build`, contrôle tidy ; validation GoReleaser ; snapshot des archives et images locales puis smoke test amd64, sans login/push registre.
- `.github/workflows/release.yml` : contrôles mise, Release Please, puis publication **dans le même workflow**, sur output `release_created` uniquement. Checkout et garde tag/SHA, upload dans draft, publication GHCR, vérification des archives re-téléchargées et index multiarch, smoke test amd64, puis finalisation/readback de la release.
- `release-please-config.json`, `.release-please-manifest.json` : composant racine et version initiale `1.1.2`, issue du fichier `VERSION` contenant `v1.1.2` et de la dernière release distante consultée. Pas de bump pré-imposé : la Release PR doit être relue.
- `.goreleaser.yaml` : OSS 2.18.2, six cibles, archives Linux/macOS tar.gz et Windows zip, SHA-256, notes Release Please conservées, GHCR linux/amd64 + linux/arm64.
- `Dockerfile`, `.dockerignore` : binaires **déjà construits** sous `linux/<arch>/`, CA préparées sur BUILDPLATFORM, image finale scratch, UID/GID 65532, contexte strictement allowlisté.
- `.github/scripts/verify-release.py` : contrôle des six archives et SHA-256 ; options pour readbacks du draft et de l'index registre.
- `.github/scripts/ensure-new-image-tag.py` : garde préalable qui refuse un tag GHCR existant, parcourt les versions du package via l'API et bloque les erreurs, **y compris les 404 ambigus**, sans suivre de redirections. Un bootstrap exact approuvé manuellement est le seul cas d'exception (voir ci-dessous). Son comportement API est testé hors ligne, pas contre le registre réel.
- `.github/scripts/tests/test_image_guard.py`, `test_verify_release.py` : tests hors ligne livrés et exécutés dans CI et dans le job check du workflow Release avant Release Please.

Ne pas remplacer `.mise.toml` par la copie de validation : il appartient à l'autre travail en cours. Les workflows exigent les tâches **`check`, `race`, `build`** et un outil `go` déclaré. La copie observée sélectionne Go **1.27.1**, tandis que `go.mod` déclare un minimum 1.26. Ces trois tâches ont été exécutées avec succès dans une copie isolée, pas dans le dépôt en travail. L'action mise installe seulement `go`, sans versions divergentes dans les workflows.

### Pourquoi la stratégie Release Please `simple`, et non `go` ?

Le langage de SPF reste Go, mais son contrat de version est un fichier texte `VERSION`. La stratégie `simple` avec `version-file: VERSION` met à jour ce fichier et CHANGELOG sans annotation supplémentaire ni faux symbole Go. La stratégie `go` utilise un updater de **source Go** lorsqu'on lui donne un version-file : ne pas lui confier arbitrairement le texte brut existant.

**Différence visible à accepter :** au premier bump, l'updater officiel `simple` écrit `1.x.y\n` dans VERSION, sans `v`, au lieu du style actuel `v1.1.2`. Le manifest est aussi sans `v` ; les tags Git/GHCR restent explicitement **`vX.Y.Z`**. La tâche mise `build-all` lue accepte cette ligne et l'utilise comme suffixe ; ses noms changent donc légèrement. Ne pas ajouter d'annotation inline au fichier : cette tâche lit la ligne entière. Si préserver ce préfixe dans VERSION est impératif, adapter séparément l'updater ou le contrat de version avant intégration, plutôt que cacher cette normalisation.

Release Please v5.0.0 embarque release-please **17.6.0** : son schéma et son bundle épinglé ont été inspectés pour `draft`, `force-tag-creation` et sorties racine. Le workflow utilise draft + création effective du tag ; GoReleaser `use_existing_draft`, `draft: true`, `mode: keep-existing` ; puis `gh release edit --draft=false` seulement après vérifications. Le job refuse les versions prérelease pour ce premier périmètre stable.

## Versions et SHA réellement vérifiés

Versions stables les plus récentes retournées par l'API officielle GitHub au moment de la préparation ; les tags d'action ont été résolus jusqu'au **commit**, pas assimilés à un SHA de tag annoté.

| Action | Version | Commit pin |
|---|---|---|
| `actions/checkout` | `v7.0.1` | `3d3c42e5aac5ba805825da76410c181273ba90b1` |
| `jdx/mise-action` | `v5.0.1` | `7a4e45a543138629540c9a1616d08632b893e492` |
| `googleapis/release-please-action` | `v5.0.0` | `45996ed1f6d02564a971a2fa1b5860e934307cf7` |
| `goreleaser/goreleaser-action` | `v7.2.3` | `f06c13b6b1a9625abc9e6e439d9c05a8f2190e94` |
| `docker/setup-buildx-action` | `v4.4.1` | `f87e5991a6d7451dcb8d9637bfbc97413f497069` |
| `docker/login-action` | `v4.6.0` | `dbcb813823bdd20940b903addbd779551569679f` |

Outils : mise **2026.9.18**, GoReleaser OSS **2.18.2**, Buildx **0.37.2**, actionlint de validation **1.7.12**. Versions et SHA des actions restent distincts des versions des outils qu'elles installent. Les archives GoReleaser/actionlint téléchargées ont été vérifiées contre leurs digests SHA-256 officiels avant exécution.

Le Dockerfile épingle l'index officiel `alpine:3.23` à `sha256:85fe1e81d6758c208f3e1eed4338a1997e19d4be002d4dd32d3100c9a8c010a0`, lu via registry-1.docker.io. L'installation APK des CA est volontairement build-time et dépend encore de l'état du dépôt Alpine : **ce n'est pas une reproductibilité hermétique complète**. Réexaminer digest/CA périodiquement. Aucun frontend Dockerfile flottant ajouté : le parser BuildKit a accepté les instructions utilisées.

La fonction OSS `dockers_v2` existe depuis 2.12 ; elle construit les manifests via Buildx et réutilise les binaires. Le templating du Dockerfile et certaines conditions avancées sont Pro, mais nous ne les utilisons pas. Les assertions SBOM/provenance avancées sont différées ; aucune demande OIDC et aucune promesse d'attestation de provenance. Le Dockerfile exécute des commandes uniquement dans l'étape CA BUILDPLATFORM : QEMU n'est pas nécessaire **pour construire** l'image arm64 de cette manière ; son exécution sur un runner amd64 reste non testée sans émulation.

## Droits : token du serveur demain ≠ GITHUB_TOKEN du job

Pour intégrer les fichiers le **2 octobre 2026**, contrôler le type du credential existant sans afficher sa valeur :

- **PAT fine-grained** : sélectionner seulement `QuentinBtd/spotify-playlist-filler`, permissions dépôt **Contents: Read and write** et **Workflows: Read and write**. **Pull requests: Read and write** seulement si ce même token doit créer/mettre à jour la PR. Metadata read est implicite. `Actions: write` n'est pas un substitut à Workflows pour pousser du YAML.
- **PAT classic / credential OAuth à scopes** : conserver le droit d'écriture approprié au dépôt (`public_repo` pour un dépôt public, ou `repo` si nécessaire) et ajouter **`workflow`**. Le scope classique `workflow` donne l'édition des fichiers Actions ; il n'est pas une permission YAML.
- Ne pas demander `admin`, gestion de secrets ou packages au token de push. Ne pas ajouter ce PAT comme secret Actions : les jobs utilisent leur propre token éphémère.

Permissions YAML effectivement préparées : **CI/check : contents read** ; **Release Please : contents write, pull-requests write, issues write** (labels documentés) ; **publish : contents write, packages write**. Aucune permission PR/issues dans publish, aucun `id-token` ou `attestations`. Vérifier que Settings → Actions autorise la création de PR par Actions ; configurer protection de main/checks requis et visibilité/association du package GHCR. Le package est initialement privé ; décider si la distribution voulue doit être publique.

Le tag/release émis par GITHUB_TOKEN ne déclenche pas une chaîne indépendante : publication basée sur les outputs dans le même workflow. Nuance documentaire actuelle : les PR `opened`/`synchronize`/`reopened` créées par GITHUB_TOKEN peuvent créer des runs **en attente d'approbation** ; prévoir de les approuver. Une GitHub App pour éviter cette étape serait une évolution, pas un prérequis caché. Aucune exécution de code PR sous `pull_request_target`, aucun secret Spotify et aucun token de publication transmis au snapshot PR.

## Docker et OAuth : limite assumée

Le code actuel bind `127.0.0.1:8080` et attend `http://127.0.0.1:8080/callback`, URL à ouvrir manuellement dans un navigateur ; tokens en mémoire seulement. **`-p 8080:8080` en bridge ne suffit pas**, puisque le listener est sur le loopback interne du conteneur. L'image n'est pas un service headless, cron ou Kubernetes fonctionnel.

Premier chemin à valider : **Docker Engine Linux local, réseau host**, navigateur sur cet hôte, port 8080 libre. Après une vraie construction/publication validée, utiliser une version ou un digest vérifié, un YAML local monté readonly, variables d'environnement à l'exécution, user non-root, capabilities supprimées et no-new-privileges. Ne pas embarquer `.env`, config réelle ou secret dans le build.

Exemple de forme de commande, **pas commande exécutée ni preuve que l'image existe** :

```sh
# IMAGE doit désigner une image locale réellement construite ou un digest publié vérifié.
# config.yml doit être lisible par UID 65532 ; ne pas rendre un fichier secret public par défaut.
docker run --rm --network=host --read-only --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --mount "type=bind,src=$PWD/config.yml,dst=/config.yml,readonly" \
  -e SPOTIFY_ID -e SPOTIFY_SECRET "$IMAGE" -config /config.yml
```

Les variables d'environnement Docker sont inspectables par les opérateurs du daemon ; un YAML secret monté avec ACL/permissions adaptées est aussi supporté. Ne pas monter le socket Docker. Ne pas utiliser de healthcheck sur le callback éphémère. Avec host, l'isolation réseau est réduite même si le listener reste loopback.

Docker Desktop documente host opt-in depuis 4.34, mais cela ne valide pas notre callback loopback précis sur macOS/Windows. Pour ces systèmes, préférer les binaires natifs tant qu'un test ou un changement applicatif n'est pas effectué. Une prise en charge bridge nécessite séparation explicite bind/redirect configurable, loopback sécurisé par défaut et publication limitée à l'hôte ; **ne pas remplacer aveuglément par 0.0.0.0**.

## Première publication GHCR : bootstrap explicitement contrôlé

Par défaut `SPF_GHCR_BOOTSTRAP_TAG` est absent/vide : un 404 de l'API bloque,
car il peut masquer un package privé existant. Aucun flag n'est activé par le
workflow ou par détection automatique d'une première version.

Pour un **package réellement neuf uniquement**, un administrateur doit avant le
merge de la Release PR :

1. Vérifier sous une identité propriétaire disposant de l'accès packages que
   `ghcr.io/quentinbtd/spotify-playlist-filler` n'existe pas déjà, y compris en
   privé, et vérifier association du dépôt et permissions du job. Une réponse
   404 du token du job ne constitue pas cette vérification.
2. Consigner l'approbation et le tag exact de cette Release PR, puis créer la
   **variable Actions du dépôt** `SPF_GHCR_BOOTSTRAP_TAG` contenant ce tag
   `vX.Y.Z` exact, jamais `true`, `*` ou une expression basée sur `RELEASE_TAG`.
   Le nom du package/owner est fixé dans le script appelé par le workflow.
3. Le script n'accepte cette exception que sur **404 de la première page** et
   égalité stricte avec `RELEASE_TAG`. Il signale que l'absence n'est pas prouvée
   par l'API. Tag existant visible, 401/403/429/5xx, réponse invalide ou erreur à
   une page suivante restent bloquants, même avec approbation.
4. Retirer immédiatement la variable après cette tentative, succès ou échec.
   Ne jamais la réactiver pour contourner une publication partielle ou un défaut
   d'accès à un package existant. Tout nettoyage/retry exige une nouvelle revue.

Cette exception est une décision humaine limitée au tag approuvé, **pas une
preuve d'absence ni un mécanisme automatique de consommation unique** : la
suppression de la variable fait partie de la procédure. Aucun réglage distant,
bootstrap, token réel ou publication n'a été testé/modifié ici.

## Corrections bloquantes du 2 octobre 2026

LOG-01 : suppression de toutes les annotations Docker (snapshot et release),
conservation des quatre labels OCI title/source/version/revision. Le snapshot
GoReleaser OSS 2.18.2 avec daemon utilise `--load` par plateforme ; l'exporteur
Docker ne produit pas d'index et ne doit pas recevoir d'annotation d'index.
Validation locale statique et `goreleaser check` seulement, **pas de build Docker**.
Sources primaires : `https://docs.docker.com/build/metadata/annotations/` ;
`https://raw.githubusercontent.com/goreleaser/goreleaser/v2.18.2/internal/pipe/docker/v2/docker.go`.

SEC-01 : 404 ambigu refusé par défaut, bootstrap ci-dessus et redirections
bloquées pour ne pas transmettre Authorization à une autre URL. Source primaire :
`https://docs.github.com/en/rest/using-the-rest-api/troubleshooting-the-rest-api`.
La chaîne Release Please/publication reste dans le même run : aucun nouveau
workflow supposé déclenché par un tag créé avec `GITHUB_TOKEN`.

## Intégration et reprise

1. Relire/adapter les fichiers après stabilisation de mise ; copier **uniquement** le payload, sans `validation/` ni copie `.git`. Préparer une PR CI/packaging avant les premiers merges de Release PR.
2. Corriger le droit d'édition des workflows par un accès limité, puis seulement pousser la PR. Cette préparation n'a pas essayé de nouveau push.
3. Vérifier une vraie CI sur GitHub avec Docker, y compris le snapshot des deux plateformes ; le code Docker n'a pas été exécuté ici faute de CLI/daemon accessible. Vérifier le smoke test et OAuth Linux host manuellement, sans playlist de production.
4. Ajouter les règles de merge et visibilité GHCR voulues. Les releases draft sont compatibles avec un mode immuable sans devoir le désactiver ; le réglage réel du dépôt n'a pas été lu.
5. Relire la Release PR, en particulier son SemVer (modernisation potentiellement incompatible). Un merge de cette PR, **après intégration**, autorisera la chaîne à publier ; pas de publication effectuée pendant la préparation.

Reprise : relancer les **jobs échoués**, conservant les sorties du job Release Please ; ne pas relancer tout le workflow en s'attendant à un second `release_created=true`. Les remplacements d'assets existants sont volontairement désactivés et la garde du tag GHCR **arrête** une nouvelle publication si l'image versionnée existe déjà. Un échec partiel peut donc empêcher une simple relance : inspecter le draft, le tag d'image et les assets, puis décider explicitement de la reprise/nettoyage des seuls éléments non publiés, sans contourner aveuglément les gardes. Il n'y a pas de dispatch de republishing générique dans ce premier payload. L'idempotence d'une publication réelle reste à tester, pas revendiquée.

## Sources primaires et preuves

- Versions/SHA : API `repos/<owner>/<repo>/releases/latest`, `git/ref/tags/<version>` puis `git/tags/<sha>` pour les tags annotés ; preuves dans `validation/action-pins.json`, fichiers `*-assets.json` et logs.
- Release Please action épinglée : `https://github.com/googleapis/release-please-action/tree/45996ed1f6d02564a971a2fa1b5860e934307cf7` ; code/schema/updaters release-please v17.6.0 dans `validation/sources/`.
- Release Please manifest : `https://github.com/googleapis/release-please/blob/v17.6.0/docs/manifest-releaser.md`
- GoReleaser OSS : `https://goreleaser.com/customization/package/dockers_v2/` et `https://goreleaser.com/customization/publish/scm/` ; source primaire versionnée et schéma **généré par le binaire 2.18.2** conservés sous `validation/sources/`.
- mise : `https://mise.jdx.dev/continuous-integration.html` ; métadonnées de l'action épinglée dans `validation/sources/mise-action.yml`.
- GitHub token/events : `https://docs.github.com/en/actions/concepts/security/github_token`
- Scopes OAuth/classic : `https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/scopes-for-oauth-apps`
- Fine-grained PAT : `https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens`
- Docker plateformes/host : `https://docs.docker.com/build/building/multi-platform/` et `https://docs.docker.com/engine/network/drivers/host/`

Des fetches goreleaser.com ont renvoyé 403, et l'extracteur web était indisponible avec ce backend ; recours aux résultats documentaires et au dépôt primaire GoReleaser **v2.18.2**. Aucune sortie Docker ni réponse de publication inventée. Voir `VALIDATION.md` pour la frontière exacte de ce qui a été testé.
