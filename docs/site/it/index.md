# Smart Stage — controllo degli spettacoli dal vivo per Mac e Windows

Riproduci contenuti audio, video e immagini sul computer e controllali dal telefono o dal tablet. Prepara una playlist nella finestra Admin dell'app, scegli gli altoparlanti e lo schermo di scena, poi avvia i contenuti con i pulsanti colorati del telecomando. I file multimediali restano sul tuo computer.

L’app è disponibile in italiano e inglese: segue la lingua del sistema e permette di cambiarla dalla barra superiore di Admin. Il telecomando segue la lingua del telefono o tablet.

Smart Stage permette di gestire la riproduzione e le immagini di scena durante gli spettacoli dal vivo. Puoi scegliere un'immagine o un video in loop come sfondo, attivare il suo audio, regolare le dissolvenze e le dissolvenze incrociate dell'audio, e attivare o disattivare lo schermo di scena in modo indipendente.

[Sito in italiano](https://arizzi74.github.io/Smart-Stage/it/) · [English](https://arizzi74.github.io/Smart-Stage/en/) · [Repository GitHub](https://github.com/arizzi74/Smart-Stage) · [Ultima versione](https://github.com/arizzi74/Smart-Stage/releases/latest)

## Installazione

Le versioni per Mac sono compilate per macOS 12 o successivo. Su Windows, la versione di riferimento è Windows 11. Per entrambe le piattaforme sono disponibili download ARM64 e Intel/AMD64.

Su Mac, incolla questo comando nel Terminale:

```sh
curl -fsSL https://raw.githubusercontent.com/arizzi74/Smart-Stage/main/install.sh | sh
```

Su Windows, incollalo in una finestra PowerShell senza privilegi di amministratore:

```powershell
irm https://raw.githubusercontent.com/arizzi74/Smart-Stage/main/install.ps1 | iex
```

I programmi di installazione rilevano l'architettura del computer, installano l'app e aprono Admin. Al termine puoi chiudere il terminale. Su Windows viene verificata la presenza del runtime Microsoft WebView2 necessario all'app. Gli aggiornamenti dell'app desktop si installano automaticamente all'avvio, prima che inizi la riproduzione.

Preferisci scaricare un archivio? Estrai lo ZIP adatto al tuo computer e apri l'app:

| Computer | Download |
| --- | --- |
| Mac — Apple Silicon | [ZIP dell'app ARM64](https://github.com/arizzi74/Smart-Stage/releases/latest/download/smartstage-darwin-arm64.app.zip) |
| Mac — Intel | [ZIP dell'app AMD64](https://github.com/arizzi74/Smart-Stage/releases/latest/download/smartstage-darwin-amd64.app.zip) |
| Windows — ARM64 | [ZIP dell'eseguibile ARM64](https://github.com/arizzi74/Smart-Stage/releases/latest/download/smartstage-windows-arm64.zip) |
| Windows — Intel / AMD | [ZIP dell'eseguibile AMD64](https://github.com/arizzi74/Smart-Stage/releases/latest/download/smartstage-windows-amd64.zip) |

Su Mac, i download effettuati dal browser possono richiedere **Apri comunque** (Open Anyway), perché l'app non è notarizzata. Il comando di installazione per Mac rimuove l'attributo di quarantena da questa app. Consulta la [guida all'installazione (in inglese)](https://github.com/arizzi74/Smart-Stage/blob/main/docs/user-guide.md#download-zips) per i permessi e i requisiti del runtime.

## Prepara uno spettacolo

1. Trascina i file multimediali dal Finder o da Esplora file nella finestra Admin dell'app, oppure premi **Scegli file…**. I file restano nella loro posizione originale.
2. Scegli l'uscita audio e lo schermo di scena. Imposta le etichette e i colori dei pulsanti e, se vuoi, un'immagine o un video come sfondo.
3. Apri **Telecomando → Impostazioni di connessione**. Configura l'URL e il token del gateway, oppure seleziona **Rete locale** e segui le indicazioni per il firewall.
4. Inquadra il codice QR con il telefono o il tablet e tocca un pulsante per avviare la riproduzione.

Le immagini possono cambiare ciò che appare sullo schermo di scena mentre la musica continua. **Attiva/Disattiva scena** controlla lo schermo di scena in modo indipendente. **STOP** torna allo sfondo corrente; **Escape** interrompe immediatamente tutto l'audio e chiude lo schermo di scena quando la finestra nativa di Smart Stage è attiva. **Esci da Smart Stage** in Admin interrompe la riproduzione e chiude l'app.

## Controllo remoto e gateway Linux

La modalità con gateway pubblico è quella predefinita. Richiede un server pubblico configurato e l'accesso a Internet; la porta del computer per il controllo remoto in ingresso dalla LAN rimane chiusa. Admin resta accessibile solo in locale su `127.0.0.1`. In alternativa, puoi scegliere il controllo sulla rete LAN locale senza gateway.

Sul tuo server Linux pubblico con systemd, installa il gateway separato per ARM64 o AMD64:

```sh
curl -fsSL https://github.com/arizzi74/Smart-Stage/releases/latest/download/install-gateway.sh | sh
```

Il programma di installazione elenca gli host HTTPS nginx esistenti compatibili e può aggiungere il percorso `/smartstage`. Se nginx non è presente, propone Caddy. Inserisci l'URL e il token di registrazione generati in Admin sul computer; Admin mostrerà un URL separato e un codice QR per il telecomando su telefono o tablet. Il gateway trasporta i comandi di controllo, mentre i file multimediali e la riproduzione restano sul computer. Per aggiornare il gateway sul server si usa il suo programma di installazione.

## Documentazione

- [Guida utente (in inglese)](https://github.com/arizzi74/Smart-Stage/blob/main/docs/user-guide.md): installazione, preparazione dello spettacolo e risoluzione dei problemi.
- [Installazione del gateway (in inglese)](https://github.com/arizzi74/Smart-Stage/blob/main/docs/gateway-install.md): configurazione del proxy HTTPS, gestione del servizio e aggiornamenti.
- [Compatibilità multimediale (in inglese)](https://github.com/arizzi74/Smart-Stage/blob/main/docs/media-compatibility.md): formati nativi e limiti di riproduzione.
- [Architettura (in inglese)](https://github.com/arizzi74/Smart-Stage/blob/main/docs/architecture.md): struttura dell'app desktop, riproduzione e controllo remoto.
- [Verifica delle versioni (in inglese)](https://github.com/arizzi74/Smart-Stage/blob/main/docs/release-verification.md): risultati delle verifiche e limiti noti dei test.
- [Segnala un problema o proponi una funzione (moduli in inglese)](https://github.com/arizzi74/Smart-Stage/issues/new/choose).
