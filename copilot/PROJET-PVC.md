# POC — Proxmox CLI/TUI Manager en Go

## 1. Contexte

L'administration quotidienne de serveurs Proxmox se fait principalement via deux interfaces :

* SSH pour les opérations système ;
* WebUI Proxmox pour les VM/LXC, snapshots, backups, stockage et certaines informations de configuration.

L'objectif du projet est de créer un outil Go léger installé directement sur chaque nœud Proxmox afin de retrouver depuis SSH les opérations Proxmox les plus courantes.

L'outil doit privilégier :

* rapidité ;
* lisibilité ;
* sécurité ;
* absence de daemon ;
* déploiement sous forme d'un binaire unique ;
* utilisation des outils natifs Proxmox ;
* possibilité future d'une interface TUI plus avancée et d'une gestion multi-nœuds.

Nom de travail :

```text
pvm
```

Proxmox CLI Manager.

---

# 2. Objectif du POC

Le POC doit permettre depuis un shell SSH de :

1. obtenir un résumé du nœud Proxmox ;
2. lister les VM et conteneurs LXC ;
3. consulter leur état ;
4. démarrer/arrêter/redémarrer un guest ;
5. consulter et gérer les snapshots ;
6. consulter les jobs de backup ;
7. consulter l'historique récent des backups ;
8. lancer manuellement un backup ;
9. consulter les stockages Proxmox ;
10. effectuer quelques contrôles de santé système ;
11. proposer une interface TUI simple.

Le POC doit fonctionner localement sur le nœud Proxmox.

Il n'est pas nécessaire dans cette première version d'implémenter un serveur HTTP, un agent permanent ou une base de données.

---

# 3. Principes d'architecture

Le programme est exécuté directement sur le nœud Proxmox :

```text
SSH
 │
 ▼
┌──────────────────────┐
│        pvm           │
│     Go binary        │
└──────────┬───────────┘
           │
     ┌─────┴──────────────────────┐
     │                            │
     ▼                            ▼
 Proxmox CLI                 Linux system
     │                            │
     ├── pvesh                    ├── /proc
     ├── qm                       ├── /sys
     ├── pct                      ├── lsblk
     ├── pvesm                    ├── df
     └── vzdump                   ├── mdadm
                                  └── uname
```

Le POC doit privilégier les commandes et interfaces natives Proxmox plutôt que réimplémenter la logique Proxmox.

Quand cela est possible, utiliser :

```bash
pvesh
```

avec une sortie JSON afin d'éviter le parsing fragile de sorties destinées aux humains.

Exemple :

```bash
pvesh get /nodes/<node>/qemu --output-format json
```

Les commandes :

```text
qm
pct
pvesm
vzdump
```

peuvent être utilisées lorsque cela simplifie une opération.

---

# 4. Contraintes

## Technique

Langage :

```text
Go
```

Cible initiale :

```text
Proxmox VE 9.x
Debian 13
amd64
```

Le programme doit être compilable sous forme d'un binaire unique.

Installation cible :

```text
/usr/local/bin/pvm
```

Configuration facultative :

```text
/etc/pvm/config.yaml
```

Aucun daemon nécessaire.

Aucune base de données.

Aucun Docker.

---

# 5. Architecture Go proposée

Structure initiale :

```text
pvm/
├── cmd/
│   └── pvm/
│       └── main.go
│
├── internal/
│   ├── proxmox/
│   │   ├── node.go
│   │   ├── guest.go
│   │   ├── vm.go
│   │   ├── container.go
│   │   ├── snapshot.go
│   │   ├── backup.go
│   │   ├── storage.go
│   │   └── command.go
│   │
│   ├── health/
│   │   ├── health.go
│   │   ├── filesystem.go
│   │   ├── raid.go
│   │   └── kernel.go
│   │
│   ├── config/
│   │   └── config.go
│   │
│   └── ui/
│       ├── table.go
│       ├── output.go
│       └── tui.go
│
├── go.mod
├── go.sum
├── Makefile
├── README.md
└── config.example.yaml
```

La logique métier ne doit pas être placée dans `main.go`.

`main.go` doit essentiellement :

1. initialiser la configuration ;
2. parser la CLI ;
3. appeler les packages internes ;
4. afficher le résultat.

---

# 6. Abstraction d'exécution des commandes

Créer une abstraction centrale permettant d'exécuter les commandes système.

Exemple conceptuel :

```go
type Runner interface {
    Run(ctx context.Context, command string, args ...string) ([]byte, error)
}
```

Cela permettra :

* de centraliser les timeouts ;
* de récupérer stdout/stderr ;
* de gérer proprement les codes retour ;
* de logger les commandes ;
* de tester le programme sans réellement exécuter `qm`, `pct` ou `pvesh`.

Ne jamais construire une commande complète passée à :

```text
sh -c
```

à partir d'une entrée utilisateur.

Utiliser directement `exec.CommandContext()` avec des arguments séparés.

---

# 7. Modèle Guest

Les VM QEMU et les conteneurs LXC doivent partager une représentation commune.

Exemple :

```go
type Guest struct {
    VMID   int
    Name   string
    Type   GuestType
    Status string
    CPU    float64
    Memory uint64
    MaxMem uint64
    Uptime int64
}
```

Types :

```text
qemu
lxc
```

Cela permet à la majorité de l'interface de ne pas avoir à distinguer VM et LXC.

---

# 8. Commandes CLI du POC

## Vue générale

```bash
pvm status
```

Exemple :

```text
NODE dg2
Proxmox 9.2.10
Kernel  7.0.14-14-pve
Uptime  37d 04h

CPU     18%
RAM     21.3 / 32 GB
ROOT    31 / 94 GB

GUESTS
ID    NAME              TYPE   STATUS
300   wp-preprod        VM     running
302   minio-preprod     VM     running
303   wp-grad-preprod   VM     stopped
305   booa-preprod      VM     running

STORAGE
local        31%    OK
local-lvm    64%    OK
backup-nas   71%    OK
```

---

# 9. Gestion des guests

Liste :

```bash
pvm vm list
```

Informations :

```bash
pvm vm status 300
```

Configuration :

```bash
pvm vm config 300
```

Actions :

```bash
pvm vm start 300
pvm vm stop 300
pvm vm shutdown 300
pvm vm reboot 300
```

Les commandes doivent automatiquement déterminer si le VMID correspond à :

```text
QEMU
```

ou :

```text
LXC
```

afin que l'utilisateur n'ait pas besoin de connaître la commande Proxmox sous-jacente.

---

# 10. Snapshots

Commandes :

```bash
pvm snapshot list 300

pvm snapshot create 300 before-upgrade

pvm snapshot delete 300 before-upgrade

pvm snapshot rollback 300 before-upgrade
```

Création avec description :

```bash
pvm snapshot create 300 before-upgrade \
    --description "Before system upgrade"
```

Si aucun nom n'est fourni :

```bash
pvm snapshot create 300
```

générer par exemple :

```text
manual-20260925-123500
```

---

# 11. Sécurité des actions destructrices

Une action destructive doit demander confirmation.

Exemple :

```text
Rollback VM 300 to snapshot "before-upgrade"

WARNING:
The current state of VM 300 may be lost.

Type VM ID to confirm: 300
```

Même principe pour :

```text
snapshot delete
snapshot rollback
vm stop
```

Un flag pourra permettre l'automatisation :

```bash
--yes
```

Exemple :

```bash
pvm snapshot delete 300 old-snapshot --yes
```

Le comportement par défaut reste interactif et sécurisé.

---

# 12. Backups

## Jobs configurés

```bash
pvm backup jobs
```

Exemple :

```text
ID             STORAGE       SCHEDULE       ENABLED
backup-daily   backup-nas    03:00 daily    yes
```

---

## Historique

```bash
pvm backup history
```

Par défaut :

```text
7 jours
```

Option :

```bash
pvm backup history --days 30
```

Exemple :

```text
DATE                 VMID   NAME              STATUS   DURATION
2026-09-25 03:02     300    wp-preprod        OK       04m21s
2026-09-25 03:14     302    minio-preprod     OK       03m12s
2026-09-25 03:21     303    wp-grad-preprod   OK       02m58s
2026-09-25 03:34     305    booa-preprod      OK       08m44s
```

L'historique doit inclure si possible :

* heure de début ;
* heure de fin ;
* durée ;
* VMID ;
* statut ;
* nœud ;
* stockage.

---

# 13. Backup manuel

Exemple :

```bash
pvm backup run 300
```

Options éventuelles :

```bash
pvm backup run 300 --storage backup-nas
```

Le programme doit afficher la progression ou au minimum suivre le résultat du `vzdump`.

Exemple :

```text
Starting backup VM 300

Storage : backup-nas
Mode    : snapshot

[....................] starting
[######..............] creating archive
[##############......] writing
[####################] done

Backup completed in 04m32s
```

Pour le POC, une vraie barre de progression n'est pas obligatoire si les informations fournies par Proxmox ne permettent pas de calculer proprement un pourcentage.

Ne jamais inventer un pourcentage.

---

# 14. Storage

Commande :

```bash
pvm storage list
```

Exemple :

```text
NAME          TYPE      USED       TOTAL      USAGE
local         dir       31 GB      94 GB       33%
local-lvm     lvmthin   412 GB     800 GB      51%
backup-nas    cifs      7.1 TB     10 TB       71%
```

Informations détaillées :

```bash
pvm storage status backup-nas
```

---

# 15. Health check

Commande importante :

```bash
pvm check
```

Elle doit agréger plusieurs contrôles simples.

Exemple :

```text
SYSTEM
[OK]   Proxmox VE 9.2.10
[OK]   kernel 7.0.14-14-pve
[OK]   uptime 37d

FILESYSTEMS
[OK]   /       31%
[WARN] /boot   78%

RAID
[OK]   md0 RAID1 [UU]
[OK]   md1 RAID5 [UUU]

STORAGE
[OK]   local       33%
[OK]   local-lvm   51%
[WARN] backup-nas  82%

BACKUPS
[OK]   VM 300 last backup 9h ago
[OK]   VM 302 last backup 9h ago
[FAIL] VM 305 last backup 3d ago

GUESTS
[OK]   4 running
[INFO] 1 stopped
```

---

# 16. Niveaux de résultat

Utiliser quatre niveaux :

```text
OK
INFO
WARN
FAIL
```

Le package health doit utiliser un modèle commun.

Exemple :

```go
type CheckResult struct {
    Name     string
    Status   Status
    Message  string
    Details  map[string]any
}
```

---

# 17. JSON

Les commandes importantes doivent pouvoir retourner du JSON.

Exemple :

```bash
pvm check --json
```

ou :

```bash
pvm status --json
```

Exemple :

```json
{
  "node": "dg2",
  "status": "warn",
  "checks": [
    {
      "name": "md0",
      "status": "ok",
      "message": "RAID1 healthy"
    },
    {
      "name": "backup-vm-305",
      "status": "fail",
      "message": "Last successful backup 3 days ago"
    }
  ]
}
```

Le JSON doit rester stable et exploitable par des outils externes.

Objectif futur :

```text
Icinga
ASE
Ansible
scripts shell
systemd timer
```

---

# 18. Codes de retour

`pvm check` doit retourner des codes utilisables dans des scripts.

Proposition :

```text
0 = OK
1 = WARN
2 = FAIL
3 = erreur interne / impossible d'effectuer les checks
```

`INFO` seul ne doit pas provoquer d'erreur.

---

# 19. Configuration

Configuration facultative :

```text
/etc/pvm/config.yaml
```

Exemple :

```yaml
health:
  filesystem:
    warning: 80
    critical: 90

  storage:
    warning: 80
    critical: 90

  backup:
    warning_after: 36h
    critical_after: 72h

ui:
  color: true
```

Le programme doit avoir des valeurs par défaut raisonnables afin de fonctionner sans fichier de configuration.

---

# 20. TUI

Lancer :

```bash
pvm
```

sans sous-commande doit ouvrir une interface TUI.

Bibliothèque recommandée :

```text
Bubble Tea
```

Interface initiale :

```text
┌─ Proxmox dg2 ───────────────────────────────────────┐
│ PVE 9.2.10 │ CPU 18% │ RAM 66% │ Disk 31%         │
├─────────────────────────────────────────────────────┤
│ ID    Guest               Type     Status           │
│                                                     │
│ 300   wp-preprod          QEMU     ● running        │
│ 302   minio-preprod       QEMU     ● running        │
│ 303   wp-grad-preprod     QEMU     ○ stopped        │
│ 305   booa-preprod        QEMU     ● running        │
│                                                     │
├─────────────────────────────────────────────────────┤
│ ↑↓ Select   Enter Details   b Backup   n Snapshot   │
│ s Start/Stop              q Quit                    │
└─────────────────────────────────────────────────────┘
```

Le POC TUI peut rester simple.

Priorité au fonctionnement de la CLI.

---

# 21. Vue détail guest

Depuis la TUI ou :

```bash
pvm vm status 300
```

afficher :

```text
VM 300 — wp-preprod

STATUS
running

RESOURCES
CPU       2 vCPU
RAM       4 GB
Disk      100 GB

UPTIME
17d 04h

SNAPSHOTS
before-upgrade
before-kernel-update

LAST BACKUP
2026-09-25 03:02
OK

NETWORK
net0
virtio
vmbr0
```

---

# 22. Détection du nœud

Le programme doit déterminer automatiquement le nœud Proxmox local.

Ne pas demander :

```bash
pvm --node dg2
```

pour les opérations locales du POC.

Une fonction dédiée doit récupérer :

```text
hostname
node Proxmox
version Proxmox
kernel
```

---

# 23. Gestion des erreurs

Les erreurs doivent être explicites.

Mauvais :

```text
error
```

Bon :

```text
Unable to create snapshot for VM 300.

Proxmox returned:
VM is locked (backup)
```

En mode debug :

```bash
pvm --debug snapshot create 300 test
```

afficher également :

```text
command
arguments
exit code
stderr
duration
```

Attention à ne jamais afficher de secrets dans les logs.

---

# 24. Logging

Mode normal :

```text
sortie concise
```

Mode verbose :

```bash
pvm -v
```

Mode debug :

```bash
pvm --debug
```

Le mode debug doit être utile pour diagnostiquer une commande Proxmox qui échoue.

---

# 25. Privilèges

Le POC est prévu pour être utilisé principalement par :

```text
root
```

sur le nœud Proxmox.

Au lancement, détecter :

```text
EUID
```

Certaines commandes en lecture peuvent éventuellement fonctionner sans root.

Les opérations nécessitant des privilèges doivent retourner un message explicite.

Ne pas tenter automatiquement une élévation avec `sudo`.

---

# 26. Sécurité

Règles impératives :

* aucune entrée utilisateur passée directement à `sh -c` ;
* validation stricte des VMID ;
* validation des noms de snapshots ;
* timeout sur les commandes externes ;
* confirmation des actions destructrices ;
* pas de secrets dans les logs ;
* pas de daemon root ;
* pas de socket réseau dans le POC ;
* pas de serveur HTTP ;
* pas de télémétrie externe.

VMID :

```text
integer positif
```

Les noms de snapshot doivent être validés avant d'être transmis aux commandes Proxmox.

---

# 27. Tests

L'abstraction `Runner` doit permettre les tests unitaires sans Proxmox.

Créer par exemple :

```go
type MockRunner struct {
    Responses map[string][]byte
}
```

Tester au minimum :

* parsing JSON `pvesh` ;
* détection QEMU/LXC ;
* parsing stockage ;
* parsing backups ;
* calcul de durée ;
* niveaux health ;
* validation VMID ;
* validation snapshot ;
* codes retour `pvm check`.

Les tests ne doivent pas nécessiter un serveur Proxmox.

---

# 28. Makefile

Prévoir :

```bash
make build
make test
make fmt
make vet
make clean
```

`make build` doit produire :

```text
bin/pvm
```

---

# 29. Installation

Prévoir éventuellement :

```bash
make install
```

qui copie :

```text
bin/pvm
```

vers :

```text
/usr/local/bin/pvm
```

Une installation manuelle doit rester possible :

```bash
cp bin/pvm /usr/local/bin/pvm
chmod 0755 /usr/local/bin/pvm
```

---

# 30. Ordre d'implémentation

Codex doit procéder progressivement.

## Phase 1 — fondations

Créer :

* projet Go ;
* CLI ;
* Runner ;
* détection Proxmox ;
* modèles internes ;
* gestion erreurs.

À la fin :

```bash
pvm version
```

doit fonctionner.

---

## Phase 2 — lecture Proxmox

Implémenter :

```bash
pvm status
pvm vm list
pvm vm status <vmid>
pvm storage list
```

Cette phase doit être stable avant d'ajouter les actions.

---

## Phase 3 — actions VM/LXC

Implémenter :

```bash
pvm vm start
pvm vm shutdown
pvm vm stop
pvm vm reboot
```

avec confirmations lorsque nécessaire.

---

## Phase 4 — snapshots

Implémenter :

```bash
pvm snapshot list
pvm snapshot create
pvm snapshot delete
pvm snapshot rollback
```

---

## Phase 5 — backups

Implémenter :

```bash
pvm backup jobs
pvm backup history
pvm backup run
```

---

## Phase 6 — health

Implémenter :

```bash
pvm check
pvm check --json
```

avec :

```text
filesystem
storage
RAID mdadm
backups
guests
kernel
```

---

## Phase 7 — TUI

Seulement lorsque la CLI est suffisamment stable.

Implémenter :

```bash
pvm
```

avec Bubble Tea.

La TUI doit utiliser les mêmes services internes que la CLI.

Ne pas dupliquer la logique Proxmox dans la TUI.

---

# 31. Hors périmètre POC

Ne pas implémenter immédiatement :

* interface Web ;
* API HTTP ;
* authentification ;
* daemon ;
* gestion distante ;
* découverte réseau ;
* orchestration de cluster ;
* migration live ;
* HA ;
* Ceph avancé ;
* gestion SDN ;
* modification complexe du hardware VM ;
* création complète de VM ;
* clone/template ;
* gestion utilisateurs Proxmox ;
* ACL ;
* firewall Proxmox ;
* mises à jour automatiques.

Ces fonctionnalités pourront être étudiées après validation du POC.

---

# 32. Évolution V2

Une V2 pourra introduire un mode distant via l'API Proxmox.

Architecture possible :

```text
                  pvm
                   │
          ┌────────┴────────┐
          │                 │
      local mode        remote mode
          │                 │
       pvesh           Proxmox API
          │                 │
          ▼                 ▼
        dg2        ┌──── cluster ────┐
                   │                 │
                  dg3               dg4
```

Exemples futurs :

```bash
pvm fleet

pvm --node dg2 status

pvm --node dg3 vm list
```

Vue fleet :

```text
NODE    PVE       KERNEL             CPU   RAM   GUESTS   BACKUP
dg2     9.2.10    7.0.14-14-pve     18%   66%      5      OK
dg3     9.x       7.0.14-11-pve      9%   43%      7      WARN
dg4     9.x       7.0.14-14-pve     22%   71%      9      OK
```

Cette architecture ne doit cependant pas complexifier le POC.

---

# 33. Critères d'acceptation du POC

Le POC est considéré fonctionnel si, installé sur un nœud Proxmox VE 9, les commandes suivantes fonctionnent :

```bash
pvm version

pvm status

pvm vm list

pvm vm status 300

pvm storage list

pvm snapshot list 300

pvm snapshot create 300 poc-test

pvm snapshot delete 300 poc-test

pvm backup jobs

pvm backup history --days 7

pvm check

pvm check --json
```

Et si :

```bash
go test ./...
```

passe sans erreur.

---

# 34. Règles pour Codex Agent

Lors de l'implémentation :

1. commencer par inspecter les interfaces disponibles sur un Proxmox VE 9 avant d'écrire des parsers fragiles ;
2. préférer les sorties JSON de `pvesh` ;
3. ne pas parser une sortie texte lorsqu'une source structurée existe ;
4. garder les commandes système derrière l'interface `Runner` ;
5. écrire les tests en même temps que les parsers ;
6. ne pas ajouter de fonctionnalités hors périmètre sans nécessité ;
7. ne pas créer de daemon ;
8. ne pas créer de serveur Web ;
9. conserver un binaire autonome ;
10. maintenir une séparation nette entre :

    * récupération des données ;
    * logique métier ;
    * affichage CLI ;
    * affichage TUI ;
11. documenter les choix lorsque l'API ou le comportement Proxmox impose un compromis ;
12. ne jamais simuler une donnée indisponible ;
13. ne jamais inventer une progression de backup ;
14. retourner les erreurs Proxmox utiles à l'utilisateur ;
15. garder le code suffisamment modulaire pour permettre ultérieurement un backend API distant.

---

# 35. Première mission Codex

Commencer uniquement par les phases 1 et 2.

Créer le squelette complet du projet puis rendre fonctionnelles :

```bash
pvm version
pvm status
pvm vm list
pvm vm status <vmid>
pvm storage list
```

Ajouter les tests unitaires correspondants.

Ne pas commencer la TUI, les snapshots ou les actions destructrices avant validation de cette première étape.

À la fin :

```bash
go test ./...
go vet ./...
```

doivent passer.

Fournir également un `README.md` expliquant :

```bash
make build
./bin/pvm status
./bin/pvm vm list
./bin/pvm storage list
```

Le code doit être utilisable sur un vrai nœud Proxmox VE 9 sans dépendance à un service externe.
