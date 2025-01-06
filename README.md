# Gui Sync

Gui Sync é uma ferramenta de sincronização de arquivos entre um diretório local e o Amazon S3. O sistema detecta automaticamente mudanças nos arquivos locais, realizando o upload apenas dos arquivos modificados, e remove arquivos do bucket que foram excluídos localmente. A ferramenta suporta agendamento de sincronização através de expressões cron e permite a geração de executáveis para Windows e Linux.

## Argumentos Necessários

Ao executar o código, é necessário passar quatro argumentos obrigatórios na linha de comando. A seguir, estão os argumentos que devem ser fornecidos ao executar o programa:

1. **Nome do Bucket S3**: Este é o primeiro argumento e deve ser o nome do bucket no Amazon S3 para onde os arquivos serão enviados.

2. **Região AWS**:O segundo argumento é a região onde o bucket S3 está localizado. Exemplos de regiões AWS: `us-east-1`, `sa-east-1`, etc.

3. **Diretório Raiz**: O terceiro argumento é o caminho para o diretório local onde os arquivos que devem ser sincronizados estão localizados. O caminho do diretório deve ser relativo ao diretório onde o comando está sendo executado.

4. **Cron Schedule**: O quarto argumento é uma expressão cron que define o agendamento para execução automática da sincronização. A aplicação permanecerá em execução e executará o processo de sincronização de acordo com o cron especificado.

### Exemplo de execução:

```bash
$ gui-sync <nome-do-bucket> <região-aws> <caminho-diretório-raiz> <cron-schedule>
```

### Exemplos práticos:

```bash
# Executar a cada 5 minutos

$ gui-sync meu-bucket sa-east-1 /home/usuario/meus-arquivos "*/5 * * * *"

# Executar todos os dias à meia-noite

$ gui-sync meu-bucket us-east-1 /path/to/dir "0 0 * * *"
```

## Arquivo `.syncignore`

O arquivo `.syncignore` é utilizado para definir padrões de arquivos ou diretórios que devem ser ignorados durante o processo de upload para o S3.

Ele funciona de maneira semelhante ao `.gitignore`, permitindo que você especifique arquivos ou diretórios que não devem ser incluídos na sincronização.

### Estrutura do .syncignore

- Cada linha do arquivo `.syncignore` pode conter um padrão de caminho.
- Comentários podem ser incluídos no arquivo começando a linha com `#`.
- Linhas em branco serão ignoradas.

Exemplo de um arquivo `.syncignore`:

```bash
# Ignorar arquivos temporários
*.tmp

# Ignorar diretório 'node_modules'
node_modules/

# Ignorar arquivo específico
config.json
```

### Comportamento do `.syncignore`

1. Se o arquivo `.syncignore` não for encontrado no diretório raiz especificado, uma mensagem será exibida: `no .syncignore file found, proceeding without ignoring files....`

2. Se o arquivo for encontrado, os padrões especificados no arquivo serão carregados e utilizados para ignorar arquivos durante o processo de upload.

3. Arquivos ou diretórios que correspondam aos padrões listados no `.syncignore` não serão enviados para o bucket S3.

## Agendamento com Cron

A aplicação aceita um cron-like schedule como parâmetro para definir quando a sincronização deve ser executada automaticamente. A aplicação permanecerá em execução e sincronizará os arquivos com base na expressão cron fornecida.

### Exemplos de Expressões Cron:

| Expressão     | Descrição                           |
| ------------- | ----------------------------------- |
| `*/5 * * * *` | Executar a cada 5 minutos           |
| `0 0 * * *`   | Executar todos os dias à meia-noite |
| `0 9 * * 1-5` | Executar às 9h de segunda a sexta   |

## Gerar Novos Executáveis

Para gerar novos executáveis compatíveis com Windows e Linux, utilize o comando `make compile`, conforme descrito no arquivo Makefile presente no projeto. O Makefile contém as instruções necessárias para compilar o código corretamente em ambas as plataformas, garantindo que os binários gerados funcionem sem problemas.

## Considerações Finais

- A aplicação é compatível com **Windows** e **Linux**.

- O processo de sincronização realiza upload de novos arquivos e remoção de arquivos que foram excluídos localmente.

- Certifique-se de que a expressão cron fornecida esteja correta para evitar problemas de agendamento.
