import os

API_KEY = os.environ["SERVICE_API_KEY"]


def main():
    print(f"using key {API_KEY}")


if __name__ == "__main__":
    main()
