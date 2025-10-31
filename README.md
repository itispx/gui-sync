# Gui Sync

Gui Sync é uma ferramenta de sincronização de arquivos entre um diretório local e o Amazon S3. O sistema detecta automaticamente mudanças nos arquivos locais, realizando o upload apenas dos arquivos modificados, e remove arquivos do bucket que foram excluídos localmente. A ferramenta suporta agendamento de sincronização através de expressões cron e permite a geração de executáveis para Windows e Linux.

# Como Usar

Ao executar o programa, ele solicitará interativamente as informações necessárias:

```bash
$ ./gui-sync
```

## Informações Solicitadas

O programa solicitará as seguintes informações, uma de cada vez:

1. **Nome do Bucket S3:** Nome do bucket no Amazon S3 para onde os arquivos serão enviados.

- Exemplo: `meu-bucket-s3`

2. **Região AWS:** Região onde o bucket S3 está localizado.

- Exemplos: `us-east-1`, `sa-east-1`, `us-west-2`

3. **Caminho do Diretório:** Caminho para o diretório local que será sincronizado.

- Exemplos: /home/usuario/meus-arquivos, . (diretório atual), C:\Users\usuario\documentos

4. **Agendamento Cron:** Expressão cron que define a frequência da sincronização automática.

- Exemplos: _/5 _ \* \* _ (a cada 5 minutos), 0 0 _ \* \* (diariamente à meia-noite)

# Funcionalidades

## Sincronização Inteligente

- **Upload Incremental:** Apenas arquivos novos ou modificados são enviados
- **Verificação de Mudanças:** Compara tamanho, data de modificação e hash MD5
- **Upload Multipart:** Arquivos maiores que 100MB usam upload multipart automático
- **Exclusão Automática:** Remove do S3 arquivos que foram deletados localmente

## Ignorar Arquivos

O próprio executável é automaticamente ignorado durante a sincronização, evitando que seja enviado para o S3.

## Arquivo `.syncignore`

O arquivo `.syncignore` é utilizado para definir padrões de arquivos ou diretórios que devem ser ignorados durante o processo de upload para o S3. Ele funciona de maneira semelhante ao `.gitignore`.

### Estrutura do .syncignore

- Cada linha do arquivo `.syncignore` pode conter um padrão de caminho
- Comentários podem ser incluídos começando a linha com `#`
- Linhas em branco são ignoradas
- O arquivo deve estar localizado no diretório raiz especificado

## Agendamento com Cron

A aplicação utiliza expressões cron para definir quando a sincronização deve ser executada automaticamente. Após a primeira sincronização, o programa permanece em execução e sincroniza os arquivos com base na expressão cron fornecida.

### Formato da Expressão Cron

```
┌───────────── minuto (0 - 59)
│ ┌───────────── hora (0 - 23)
│ │ ┌───────────── dia do mês (1 - 31)
│ │ │ ┌───────────── mês (1 - 12)
│ │ │ │ ┌───────────── dia da semana (0 - 6) (Domingo=0)
│ │ │ │ │
│ │ │ │ │
* * * * *
```

### Exemplos de Expressões Cron

| Expressão      | Descrição                             |
| -------------- | ------------------------------------- |
| `*/1 * * * *`  | Executar a cada 1 minuto              |
| `*/5 * * * *`  | Executar a cada 5 minutos             |
| `*/15 * * * *` | Executar a cada 15 minutos            |
| `0 * * * *`    | Executar a cada hora (início da hora) |
| `0 0 * * *`    | Executar todos os dias à meia-noite   |
| `0 9 * * 1-5`  | Executar às 9h de segunda a sexta     |
| `0 12 * * *`   | Executar todos os dias ao meio-dia    |
| `0 0 1 * *`    | Executar no primeiro dia de cada mês  |
| `0 0 * * 0`    | Executar todo domingo à meia-noite    |

### Gerar Novos Executáveis

Para gerar novos executáveis compatíveis com Windows e Linux, utilize o comando `make compile`, conforme descrito no arquivo Makefile presente no projeto.

```bash
$ make compile
```

O Makefile contém as instruções necessárias para compilar o código corretamente em ambas as plataformas, garantindo que os binários gerados funcionem sem problemas.

# Características Técnicas

- **Upload Concorrente:** Até 5 arquivos simultaneamente
- **Threshold Multipart:** 100 MB
- **Tamanho de Parte:** 50 MB
- **Concorrência de Partes:** 3 partes simultâneas
- **Retries Automáticos:** Até 10 tentativas
- **Timeout por Request:** 5 minutos
- **Compatibilidade:** Windows e Linux

# Requisitos

- Go 1.16 ou superior (para compilação)
- Credenciais AWS válidas
- Permissões necessárias no bucket S3:
  - `s3:PutObject`
  - `s3:GetObject`
  - `s3:DeleteObject`
  - `s3:ListBucket`
