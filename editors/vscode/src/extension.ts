import * as fs from 'fs';
import * as path from 'path';
import * as vscode from 'vscode';
import {
	Executable,
	LanguageClient,
	LanguageClientOptions,
	ServerOptions,
	TransportKind,
} from 'vscode-languageclient/node';

let client: LanguageClient | undefined;

/**
 * Every language id a SQL file is likely to carry: VS Code's built-in `sql`,
 * plus the ids popular dialect and templating extensions register. Unknown
 * languages are harmless — the selector simply never matches them.
 */
const LANGUAGES = [
	'sql',
	'jinja-sql',
	'sql-bigquery',
	'dbt-sql',
	'pgsql',
	'mysql',
	'plsql',
];

/**
 * Locate the rusq binary: an explicit `rusq.binaryPath` wins, then a binary
 * shipped next to the extension, then whatever `rusq` resolves to on PATH.
 */
function resolveBinary(): string {
	const configured = vscode.workspace.getConfiguration('rusq').get<string>('binaryPath', '').trim();
	if (configured.length > 0) {
		return configured;
	}
	// A platform-specific package ships the matching binary at the extension
	// root (rusq.exe on Windows).
	const names = process.platform === 'win32' ? ['rusq.exe', 'rusq'] : ['rusq'];
	for (const name of names) {
		const bundled = path.join(__dirname, '..', name);
		if (fs.existsSync(bundled)) {
			return bundled;
		}
	}
	return 'rusq';
}

function serverOptions(): ServerOptions {
	const args = ['server'];
	const configPath = vscode.workspace.getConfiguration('rusq').get<string>('configPath', '').trim();
	if (configPath.length > 0) {
		args.push('--config', configPath);
	}
	const executable: Executable = {
		command: resolveBinary(),
		args,
		transport: TransportKind.stdio,
	};
	return { run: executable, debug: executable };
}

function clientOptions(): LanguageClientOptions {
	return {
		// The server infers the dialect and templater per file from .rusq.toml
		// discovery, so the same selector covers every SQL dialect.
		documentSelector: [
			...LANGUAGES.map((language) => ({ scheme: 'file', language })),
			...LANGUAGES.map((language) => ({ scheme: 'untitled', language })),
		],
		outputChannelName: 'rusq',
	};
}

export async function activate(context: vscode.ExtensionContext): Promise<void> {
	client = new LanguageClient('rusq', 'rusq', serverOptions(), clientOptions());

	context.subscriptions.push(
		vscode.commands.registerCommand('rusq.restartServer', async () => {
			await restart();
		}),
		vscode.commands.registerCommand('rusq.showOutput', () => {
			client?.outputChannel.show(true);
		}),
		// A settings change (binary path, --config) needs a fresh server.
		vscode.workspace.onDidChangeConfiguration(async (event) => {
			if (event.affectsConfiguration('rusq.binaryPath') || event.affectsConfiguration('rusq.configPath')) {
				await restart();
			}
		}),
	);

	try {
		await client.start();
	} catch (err) {
		const message = err instanceof Error ? err.message : String(err);
		const choice = await vscode.window.showErrorMessage(
			`rusq: the language server failed to start (${message}). Is the rusq binary installed?`,
			'Show output',
			'Open settings',
		);
		if (choice === 'Show output') {
			client.outputChannel.show(true);
		} else if (choice === 'Open settings') {
			await vscode.commands.executeCommand('workbench.action.openSettings', 'rusq.binaryPath');
		}
	}
}

async function restart(): Promise<void> {
	if (!client) {
		return;
	}
	await client.stop();
	client = new LanguageClient('rusq', 'rusq', serverOptions(), clientOptions());
	await client.start();
}

export async function deactivate(): Promise<void> {
	if (client) {
		await client.stop();
		client = undefined;
	}
}
