# SPF — proposition CI/CD et livraison Docker

**Décision proposée et fichiers préparés hors dépôt — 1er octobre 2026.** Dépôt examiné en lecture seule : `/root/spotify-playlist-filler`, branche `refactor/go-project-conventions`. Depuis l’extension d’autorisation, workflows et Dockerfile sont préparés exclusivement dans le dossier scratch ; aucun tag, secret, réglage GitHub ou publication créé. `INTEGRATION.md` et `VALIDATION.md` font foi pour le payload et les validations désormais réalisées. La migration vers mise étant menée en parallèle, les noms de tâches ci-dessous constituent un contrat à confirmer après cette migration.

## 1. Recommandation

**Conserver Release Please pour décider et faire relire les versions ; ajouter GoReleaser OSS pour fabriquer et livrer les artefacts.** Ce sont deux responsabilités complémentaires, pas deux concurrents : Release Please automatise version, changelog, Release PR, tag et GitHub Release ; GoReleaser automatise compilation, archives et livraison à partir d'un tag. [S1, S2, S3]

Pour SPF, je recommande GitHub Actions + mise + Release Please + GoReleaser OSS + GHCR, sans Docker Hub, serveur de déploiement, licence Pro ni secret Spotify en CI. « CD » signifie ici distribution d'un CLI, **pas exécution automatique de synchronisations Spotify**. Les binaires natifs restent la voie d'utilisation principale ; Docker est une distribution complémentaire avec une limite OAuth explicite.

### Comparaison

| Sujet | Release Please + GoReleaser | GoReleaser seul |
|---|---|---|
| Choix de version | Proposition SemVer issue des Conventional Commits, relue dans une Release PR [S1] | Le mainteneur choisit et pousse le tag ; GoReleaser consomme le tag [S3] |
| Changelog | Historisé et relu avant livraison [S1] | Notes générables entre tags lors de la livraison [S4] ; un changelog versionné exige une convention supplémentaire |
| Binaires et conteneurs | GoReleaser fait cette partie | Même moteur et mêmes capacités |
| Déclenchement | Publication conditionnelle dans le workflow qui exécute Release Please | Publication sur un tag créé par un humain |
| Coût de maintenance proposé | Deux configurations, mais conserve le fonctionnement déjà connu de Quentin | Moins d'outils, mais responsabilité manuelle de versionnement |

**GoReleaser seul est le meilleur choix si l'objectif prioritaire devient « je pousse un tag quand je veux publier ».** Il ne remplace pas à lui seul l'expérience Release PR de Release Please. Puisque Quentin utilise déjà celle-ci, changer de modèle n'apporte pas assez de valeur ici. [S1, S3]

## 2. État réellement observé et blocages

Observations issues des fichiers et commandes locales, distinctes des recommandations :

- `go.mod` déclare Go 1.26 ; le CLI est `./cmd/spotify-playlist-filler`. `VERSION` contient `v1.1.2`, mais cela ne prouve pas que cette version corresponde à la dernière release distante.
- Le Makefile lu définit tests, vet, contrôle gofmt, race et build ; `main.go` n'expose actuellement aucune variable de version destinée à `ldflags` ni option de version.
- L'authentification écoute **`127.0.0.1:8080`**, avec callback **`http://127.0.0.1:8080/callback`** (`internal/spotifyauth/login.go:20–21,74`). L'URL est imprimée ; il faut l'ouvrir dans un navigateur. Les tokens restent en mémoire et une nouvelle invocation exige une connexion.
- Le fichier YAML reste obligatoire. Les identifiants peuvent venir de `SPOTIFY_ID` et `SPOTIFY_SECRET` ; le programme lit mais ne réécrit pas le YAML (`internal/config/config.go`). La synchronisation peut supprimer des titres : ne pas tester sur des playlists personnelles en CI.
- `gh pr view 8 --json number,state,files,url` confirme une PR ouverte **sans fichier `.github/workflows/…`** au moment de la lecture. Aucun succès de CI n'est revendiqué.
- Le rejet d'un premier push faute d'autorisation de modification des workflows est un **fait transmis dans le contexte**, pas une tentative répétée pendant cette étude. La branche locale `backup/spf-modernization-with-ci` existe et contient `.github/workflows/ci.yml`. Le brouillon `/root/.hermes/cache/scratch/spf-ci-workflow.yml` existe ; il utilise encore Go directement et `build.sh`, et ne constitue pas la future CI mise.
- **Docker n'est pas disponible via la CLI sur cette machine** : `docker version` et `docker buildx version` retournent `docker: command not found` ; aucun socket `/var/run/docker.sock` n'a été trouvé. Cela ne démontre pas l'absence de tout daemon distant, mais empêche ici une validation Docker. Rien n'a été installé.

Deux autorisations à ne pas confondre : pouvoir **pousser des fichiers de workflow** avec les credentials du développeur/serveur, et permissions du **`GITHUB_TOKEN` éphémère pendant un job**. Ajouter `contents: write` au YAML ne résout pas le rejet du push. Faire intégrer la future PR CI par un moyen déjà autorisé, ou décider explicitement d'un accès limité à ce dépôt et à l'édition des workflows ; ne pas élargir aveuglément le token actuel.

## 3. CI proposée : les mêmes tâches localement et sur GitHub

Prévoir une CI sur `pull_request` et sur les pushes de `main`, avec runner GitHub hébergé Linux, timeout et cache Go. Installer une version de mise épinglée ; faire sélectionner les outils par `.mise.toml`, en cohérence avec `go.mod`. La documentation mise recommande la même configuration en développement et CI, avec `mise run`/`mise exec`, sans activation interactive du shell. Un lockfile, s'il est adopté, permet `mise install --locked`. [S5]

Contrat de tâches proposé :

- `fmt` : reformater **localement**, jamais corriger silencieusement une PR en CI.
- `check` : refuser un format incorrect, lancer vet et tests déterministes. Ajouter un contrôle de cohérence `go.mod`/`go.sum` sans committer les modifications produites.
- `race` : tests avec détecteur de races sur Linux natif ; conserver le support CGO requis pour cette tâche, indépendamment du choix de binaires de livraison sans CGO.
- `build` : compiler le CLI local. À terme, un contrôle de release GoReleaser doit vérifier également toutes les cibles, plutôt que conserver deux listes divergentes de plateformes.

Le workflow appelle ces tâches, pas une copie de leurs commandes. Exiger le check CI avant merge. Commencer avec cette CI compacte ; ajouter des smoke tests natifs macOS et Windows avant d'affirmer une compatibilité d'exécution validée. La cross-compilation seule ne suffit pas.

Une fois la livraison configurée, ajouter en PR `goreleaser check` et un snapshot **sans droits d'écriture**, puis les smoke tests image. `goreleaser release --snapshot --clean` est le mode local-only documenté [S3]. Attention : avec `dockers_v2`, `--skip=publish` supprime aussi la construction des images ; ce n'est donc pas une preuve de validation Docker. [S6]

## 4. Release : une seule chaîne, pas de déclenchement implicite par tag

Proposer deux workflows conceptuels : CI pour les contributions ; release sur push de `main`, contenant ses propres contrôles et les jobs Release Please/publication. Pas de dépendance fragile à un événement de release émis par un autre workflow.

Flux recommandé :

1. Un push humain sur `main` passe les tâches CI partagées.
2. Le job Release Please maintient une Release PR pour le composant racine (`.`), stratégie `simple` avec `version-file: VERSION` pour maintenir le fichier texte existant. Le premier bump retire intentionnellement le préfixe `v` dans ce fichier ; les tags restent `vX.Y.Z`. Adopter des titres de PR Conventional Commits et squash merge. Revoir le caractère éventuellement incompatible de la modernisation, sans inventer le prochain numéro de version. [S1, S2]
3. Le merge humain de la Release PR provoque un nouveau push sur `main`. Release Please crée la release ; exposer ses sorties **`release_created`, `tag_name`, `sha`** comme outputs du job. [S2]
4. Dans **le même workflow**, un job de publication dépend du job précédent et ne démarre que lorsque `release_created == 'true'`. Checkout du tag exact avec historique et tags complets ; vérifier sa correspondance avec le SHA retourné. GoReleaser construit depuis ce tag, jamais depuis un `main` qui aurait avancé.
5. GoReleaser produit les archives, checksums et image GHCR. Conserver les notes Release Please via `release.mode: keep-existing`, sans second changelog concurrent. [S4]

**Pourquoi ce chaînage ?** Les événements `push`/release causés par `GITHUB_TOKEN` ne déclenchent pas normalement un autre workflow. Il existe aujourd'hui une nuance importante : GitHub documente des runs PR `opened`/`synchronize`/`reopened` créés par ce token **en attente d'approbation humaine**, ainsi que les exceptions dispatch. Le README Release Please généralise encore l'absence de déclenchement ; pour ce comportement GitHub, privilégier la documentation GitHub actuelle. Prévoir l'approbation des checks de la Release PR, sans contourner la protection de branche. Un token d'installation GitHub App peut automatiser ces checks plus tard si le besoin est réel ; il n'est pas nécessaire pour chaîner la publication dans le même workflow. [S7]

### Éviter les releases publiques incomplètes

Je privilégie un **brouillon de release**, rempli puis publié après vérification : Release Please propose `draft: true` et `force-tag-creation: true` en mode manifest ; GoReleaser propose `use_existing_draft: true`. Garder le brouillon jusqu'à la vérification des assets et du registre, puis le finaliser dans le job de publication. Ce petit supplément évite une release annoncée sans binaires et respecte les releases immuables, dont les assets ne doivent plus changer après publication. Vérifier ces options contre les versions effectivement épinglées avant implémentation ; le réglage d'immutabilité du dépôt n'a pas été inspecté. Ne pas le désactiver pour contourner un upload. [S8, S4, S9]

Initialiser le manifest de version seulement après comparaison de `VERSION`, des tags et releases existants. Préférer tag/manifest comme source de vérité ; retirer ultérieurement `VERSION` si la migration le rend inutile, ou configurer explicitement sa maintenance. Ne pas supposer que la stratégie `go` met à jour arbitrairement ce fichier. [S8]

Pour reprendre une publication interrompue, relancer le job échoué avec le même tag et SHA. Une réexécution complète de Release Please peut ne plus rendre `release_created=true` : prévoir un chemin manuel contrôlé sur un **tag existant validé**, sans nouveau bump. Ne jamais déplacer un tag ni remplacer silencieusement des assets déjà publiés. Sérialiser les releases et ne pas annuler un job en cours de publication.

## 5. Artefacts : GoReleaser OSS suffit

Cibles décidées pour SPF :

| OS | Architectures | Format proposé |
|---|---|---|
| Linux | amd64, arm64 | tar.gz |
| macOS (`darwin`) | amd64, arm64 | tar.gz |
| Windows | amd64, arm64 | zip, binaire `.exe` |

Prévoir six archives, `checksums.txt` SHA-256, README, licence et exemple YAML **sans identifiants**. Builder sur `./cmd/spotify-playlist-filler`, `-trimpath`, `CGO_ENABLED=0` pour les artefacts, sous réserve de validation. Ne pas ajouter des `ldflags` vers des symboles inexistants : une option `--version` sera un changement applicatif distinct, non réalisé ici. GoReleaser documente la construction et l'archivage par cible. [S3]

**La publication multiarchitecture Docker n'est pas réservée à Pro.** La documentation actuelle décrit `dockers_v2` depuis GoReleaser v2.12, utilisant Buildx et les binaires déjà construits ; cibler explicitement `linux/amd64` et `linux/arm64`. Le nom est provisoire avant v3. Éviter les anciennes configurations `dockers`/`docker_manifests`, dépréciées. Pro apporte notamment templating avancé et split/merge ; aucun de ces besoins n'est nécessaire ici. [S6, S10, S11]

Proposition de registre : `ghcr.io/quentinbtd/spotify-playlist-filler`, tag versionné `vX.Y.Z` et digest publié dans le compte rendu de livraison. Pas de `latest` au départ : les consommateurs choisissent une version ou un digest. Vérifier association du package au dépôt et visibilité voulue : GHCR crée initialement les packages privés. [S12]

## 6. Docker : image minimale, OAuth local assumé

Concevoir une image Linux non-root contenant le binaire GoReleaser et un bundle CA pour HTTPS, sans SDK, sources, shell ni credentials. Une étape de préparation des CA peut précéder une image finale `scratch` ; fixer un UID/GID numérique et des permissions de fichiers lisibles par cet utilisateur. Le Dockerfile d'emballage ne recompilera pas le Go. Épingler les bases par digest et organiser leurs mises à jour, sinon on fige aussi leurs vulnérabilités. Utiliser un ENTRYPOINT en forme exec pour les signaux. [S6, S13, S14]

Configuration montée en lecture seule, chemin passé à `-config`. Aucun `config.yml` réel ni `.env` dans le contexte/image ; si un contexte complet est introduit plus tard, ajouter une exclusion explicite. Fournir les credentials à l'exécution seulement. Des variables d'environnement ne sont pas un coffre : les opérateurs ayant accès à Docker peuvent les inspecter ; un YAML secret monté avec permissions limitées est aussi possible avec le code actuel. Aucun volume de token n'est utile tant qu'il n'y a pas de persistance implémentée.

### Le piège de `-p 8080:8080`

**Publier le port ne corrige pas le bind loopback interne.** Dans le réseau bridge, l'application écoute sur le loopback du conteneur, pas son interface réseau ; rediriger un port vers le conteneur ne la rend donc pas joignable comme attendu. De plus, le callback `127.0.0.1` du navigateur désigne la machine du navigateur. Cette conclusion est une déduction du code SPF et du modèle réseau Docker. [S15]

Chemin initial proposé, à tester : **Docker Engine sur Linux local avec `--network=host`**, navigateur sur cet hôte et port 8080 libre. Le partage de namespace réseau permet de garder le callback et le bind loopback actuels ; aucune publication `-p` nécessaire. Ce choix réduit l'isolation réseau, donc ne l'offrir que pour une image de confiance, sans privilèges, capabilities inutiles ni montage du socket Docker. [S16]

Docker Desktop documente aussi un mode host opt-in depuis 4.34, avec limitations ; ne pas en déduire que notre bind spécifique est déjà validé sur macOS/Windows. Pour ces plateformes, recommander d'abord le **binaire natif**. Une vraie prise en charge bridge multiplateforme demande un changement applicatif explicite : séparer adresse d'écoute et redirect URI, valider la configuration, conserver loopback par défaut et limiter la publication côté hôte à `127.0.0.1`. Ne jamais remplacer aveuglément le bind par `0.0.0.0` ou exposer le callback sur le LAN. [S15, S16]

L'image ne transforme pas SPF en service headless : navigateur et callback doivent être accessibles sur la même machine, et chaque invocation requiert une autorisation. Exécution distante, cron ou Kubernetes nécessiteraient une conception OAuth/stockage de tokens séparée. Le conteneur n'est pas un daemon ; ne pas proposer de healthcheck permanent sur un callback temporaire.

## 7. Permissions et sécurité

Politique proposée, fondée sur le moindre privilège [S17] :

| Job | Permissions à accorder explicitement |
|---|---|
| CI / snapshots PR | `contents: read` uniquement |
| Release Please | `contents: write`, `pull-requests: write` ; son README recommande aussi `issues: write` pour le fonctionnement documenté avec labels : vérifier les appels et ne pas promettre que son retrait est sûr sans test [S2] |
| Livraison assets + GHCR | `contents: write`, `packages: write`, sans droits PR/issues [S18] |

Utiliser `GITHUB_TOKEN` pour GHCR, pas un PAT personnel permanent. Les permissions de création de PR doivent aussi être autorisées dans les réglages Actions. [S2, S12]

Toutes les actions, y compris checkout, mise, Release Please et Docker, seront épinglées à un SHA complet vérifié, avec commentaire de version et mises à jour revues. Permissions read-only par défaut, élévation par job, checkout sans conservation de credentials quand inutile, runners éphémères. Pas de secret Spotify en CI, ni secrets de publication pour une PR de fork ; pas de `pull_request_target` exécutant du code non fiable. Séparer les caches PR et publication si un risque d'empoisonnement apparaît. [S17]

Reporter attestations/signatures avancées à une deuxième étape ; ne pas demander dès maintenant `id-token: write` et `attestations: write` pour une chaîne qui ne les utilise pas. Les checksums vérifient l'intégrité des fichiers, ils ne constituent pas à eux seuls une preuve d'identité du producteur.

## 8. Mise en œuvre future et critères d'acceptation

Après accord, dans des PR distinctes et sans conflit avec la migration mise :

1. **CI seule** : tâches communes stabilisées, push de workflow par un accès autorisé, refus démontré sur format/test incorrect, passage démontré sur une contribution saine et une PR fork sans secrets.
2. **Packaging sans publication** : configuration GoReleaser OSS épinglée, six archives contrôlées, checksums recalculés, smoke tests `--help` natifs. Confirmer la séparation CGO race / builds statiques.
3. **Docker local sans push** sur machine équipée : Buildx disponible, snapshots séparés par architecture, `--help`, exécution non-root, lecture YAML readonly et annulation par signal. Tester les deux architectures nativement ou via émulation clairement signalée ; inspecter bundle CA et absence de credentials.
4. **OAuth Linux host**, manuellement et avec autorisation dédiée : URL imprimée, callback loopback, état invalide refusé, timeout/annulation et fermeture du listener. Aucun test d'écriture Spotify automatique ; tout test réel de synchronisation exige une playlist jetable explicitement autorisée.
5. **Publication réelle uniquement après accord** : vérifier outputs, tag/SHA, conservation des notes, assets, digest/index GHCR avec deux plateformes, récupération des artefacts, puis finalisation du brouillon. Vérifier aussi une reprise après échec partiel avant d'annoncer la chaîne robuste.

**Ce qui est validé maintenant :** architecture argumentée, comportement OAuth lu dans le code, inventaire PR/brouillons, documentation primaire consultée et absence de Docker CLI local constatée. **Ce qui ne l'est pas :** YAML de workflow final, image construite, OAuth en conteneur, CI distante réussie, permissions exactes en exécution ou publication. Ce document ne prétend pas les remplacer.

## Sources primaires consultées le 1er octobre 2026

Références documentaires (URLs conservées en code pour identification) :

- **S1** — Release Please, responsabilités et Release PR : `https://github.com/googleapis/release-please`
- **S2** — Release Please Action, outputs racine, permissions, stratégie Go : `https://github.com/googleapis/release-please-action`
- **S3** — GoReleaser, tags, archives et snapshots : `https://goreleaser.com/getting-started/quick-start/`
- **S4** — GoReleaser, releases existantes, drafts et conservation des notes : `https://goreleaser.com/customization/publish/scm/`
- **S5** — mise, configuration commune et installation en CI : `https://mise.jdx.dev/continuous-integration.html`
- **S6** — GoReleaser Docker v2, plateformes, contexte et tests snapshot : `https://goreleaser.com/customization/package/dockers_v2/`
- **S7** — GitHub, déclenchements par GITHUB_TOKEN et exceptions PR/dispatch : `https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/trigger-a-workflow`
- **S8** — Release Please, manifest, bootstrap, draft et force-tag-creation : `https://github.com/googleapis/release-please/blob/main/docs/manifest-releaser.md`
- **S9** — GitHub, releases immuables et séquence draft/assets/publication : `https://docs.github.com/en/code-security/concepts/supply-chain-security/immutable-releases`
- **S10** — GoReleaser, ancienne intégration Docker dépréciée : `https://goreleaser.com/customization/package/docker/`
- **S11** — GoReleaser Pro, capacités supplémentaires : `https://goreleaser.com/pro/`
- **S12** — GitHub, GHCR, authentification et visibilité : `https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry`
- **S13** — Docker, non-root, ENTRYPOINT et bases épinglées : `https://docs.docker.com/build/building/best-practices/`
- **S14** — Docker, certificats CA dans les images : `https://docs.docker.com/engine/network/ca-certs/`
- **S15** — Docker, publication et adressage des ports : `https://docs.docker.com/engine/network/port-publishing/`
- **S16** — Docker, réseau host et limites Desktop : `https://docs.docker.com/engine/network/drivers/host/`
- **S17** — GitHub, moindre privilège, SHA et code non fiable : `https://docs.github.com/en/actions/reference/security/secure-use`
- **S18** — GitHub, permissions de publication de conteneurs : `https://docs.github.com/en/actions/tutorials/publish-packages/publish-docker-images`

Les chemins et lignes SPF cités dans le texte sont des observations locales, pas des capacités promises par ces documentations. Les comportements de versions futures ne sont pas présumés.


## Correctif ciblé du 2 octobre 2026

Seuls LOG-01/SEC-01 de la revue sont corrigés : pas d'annotations Docker,
labels OCI conservés ; garde 404 fail-closed, exception manuelle pour un nouveau
package limitée au tag exact et redirections bloquées. Les tests des deux scripts
sont désormais livrés et exécutés par les jobs de contrôle. Procédure de première
publication et limites dans `INTEGRATION.md`, résultats réels dans `VALIDATION.md`.
Aucun changement applicatif, de mise, de déclenchement par GITHUB_TOKEN, de
publication, de provenance ou de reprise automatique.

## Extension exécutée : fichiers prêts à intégrer

Voir `INTEGRATION.md` pour le payload exact, les pins vérifiés, le contrat mise et les droits du 2 octobre 2026. Voir `VALIDATION.md` pour les validations réelles : actionlint, schémas, GoReleaser check, tâches mise et snapshot six cibles sans Docker ; aucune publication ni build Docker. La comparaison stratégique reste valable ; les constats « non testé » de la proposition initiale sont remplacés, pour les seuls contrôles effectivement listés, par ce rapport de validation.
