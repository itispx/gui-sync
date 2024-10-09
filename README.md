# Gui Sync

## Argumentos Necessários

Ao executar o código, é necessário passar três argumentos obrigatórios na linha de comando. A seguir, estão os argumentos que devem ser fornecidos ao executar o programa:

1. Nome do Bucket S3 (`bucketName`):
   Este é o primeiro argumento e deve ser o nome do bucket no Amazon S3 para onde os arquivos serão enviados.

2. Região AWS (`region`):
   O segundo argumento é a região onde o bucket S3 está localizado. Exemplo de regiões AWS: `us-east-1`, `sa-east-1`, etc.

3. Diretório Raiz (`rootDir`):
   O terceiro argumento é o caminho para o diretório local onde os arquivos que devem ser sincronizados estão localizados.

## Exemplo de execução:

```bash
$ gui-sync <nome-do-bucket> <região-aws> <caminho-diretório-raiz>
```

### Exemplo prático:

```bash
$ gui-sync meu-bucket sa-east-1 /home/usuario/meus-arquivos
```

Se algum desses argumentos não for fornecido, o programa exibirá a mensagem de erro "not enough arguments." e será encerrado.

## Arquivo `.syncignore`

O arquivo `.syncignore` é utilizado para definir padrões de arquivos ou diretórios que devem ser ignorados durante o processo de upload para o S3.

Ele funciona de maneira semelhante ao `.gitignore`, permitindo que você especifique arquivos ou diretórios que não devem ser incluídos na sincronização.

## Estrutura do `.syncignore`

- Cada linha do arquivo `.syncignore` pode conter um padrão de caminho.
- Comentários podem ser incluídos no arquivo começando a linha com `#`.
- Linhas em branco serão ignoradas.

## Exemplo de um arquivo .syncignore:

```bash
Copy code
# Ignorar arquivos temporários
*.tmp

# Ignorar diretório 'node_modules'
node_modules/

# Ignorar arquivo específico
config.json
```

Neste exemplo:

- Qualquer arquivo com a extensão `.tmp` será ignorado.
- O diretório `node_modules/` será ignorado.
- O arquivo `config.json` será ignorado.

## Comportamento do `.syncignore`

1. Se o arquivo `.syncignore` não for encontrado no diretório raiz especificado, uma mensagem será exibida: `no .syncignore file found, proceeding without ignoring files...`.
2. Se o arquivo for encontrado, os padrões especificados no arquivo serão carregados e utilizados para ignorar arquivos durante o processo de upload.
3. Arquivos ou diretórios que correspondam aos padrões listados no `.syncignore` não serão enviados para o bucket S3.

## Considerações:

- O arquivo `.syncignore` deve estar localizado no diretório raiz definido pelo argumento `rootDir`.
- O código não trata de padrões complexos, então o comportamento padrão é comparar exatamente o caminho do arquivo com as entradas no `.syncignore`.

Com essas configurações e informações, o código realiza o upload dos arquivos para o bucket S3, ignorando aqueles que seguem os padrões definidos no `.syncignore`.
