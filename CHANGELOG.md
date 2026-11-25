# Changelog

Notable changes of pve-console, by release. The entries are the summaries of the commits.

## v0.2.3 - 2026-11-25

### Features

- Affiche l'IPv4 et l'état de l'agent d'un invité
- Bascule le guest agent d'une VM avec la touche A

### User experience

- Affiche la touche A à côté de « agent off »


## v0.2.2 - 2026-11-25

### Fixes

- Lit les facteurs 2FA via /access/tfa/<userid>

### Configuration

- Fixe l'identité des commits publics (CultureLinux)


## v0.2.1 - 2026-11-25

### Features

- Changement de mot de passe d'un utilisateur depuis la TUI
- Enrôlement TOTP avec QR code depuis la vue Users

### Fixes

- Lit le CPU des invités dans /cluster/resources
- Met à jour l'indicateur 2FA de la liste après une désactivation
- Corrige le chemin d'ajout du TOTP et nomme le compte avec le nœud

### User experience

- Confirme la désactivation du 2FA avec enter au lieu de retaper l'identifiant


## v0.2.0 - 2026-11-25

### Features

- Version, détection et mise à jour de pve-console depuis la TUI
- Releases versionnées, publication scriptée, installateur Proxmox
- Mise à jour guidée des paquets : index, confirmation, direct, reboot
- Menu Log : erreurs et résultats des tâches, proxmox et pve-console
- Journalise les tâches Proxmox en échec du cluster
- Journal des erreurs du TUI (vue E), source proxmox / pve-console
- Suivi des tâches Proxmox, reboot/shutdown et checks matériels
- Ajoute les commandes CLI vm, snapshot, backup, updates, user et tfa

### Fixes

- Retire l'état RAM du snapshot d'une VM avec virtiofs
- Sonde le type de guest avant chaque action
- Charge la liste des snapshots dès la création et après une tâche perdue
- Allonge le timeout du snapshot TUI et corrige le libellé d'upgrade
- Durcit restore, backup, upgrade et timeouts CLI

### Refactoring

- Découpe internal/ui/tui.go en fichiers par responsabilité

### Documentation

- README vitrine et captures animées générées depuis la démo
- Aligne le nom pve-console, la doc de publication et la CI

### Style

- Refonte visuelle et ergonomique du TUI, mode démo

### CI

- Rend publish:github tolérant aux 504 de GitLab

### Other

- Github action
- Clean
- Rewrite ui
- Build YT
- Fix ci depth
- Fix ci auto publish
- Prepa push github
- Preparation public
- Restauration backup + snapshot
- Better loader
- Loader + backup
- Modal backup full
- Modal overlay
- Fichier rc
- Search + help + splashscreen
- Ajot backup
- Ajout de ci
- Amelioration tui
- Version tui + navigation
- Version tui + backup update
- Version tui + snap
- Version tui
- Ci fix go versio
- Ci
- Sprint 3
- Sprint 2
- Sprint 1
- Strint 0
- Init pvc

