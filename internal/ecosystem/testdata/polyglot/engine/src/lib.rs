pub struct Inventory {
    pub stock: u32,
}

impl Inventory {
    pub fn take(&mut self, n: u32) -> bool {
        if n > self.stock {
            return false;
        }
        self.stock -= n;
        true
    }
}

pub fn restock(inv: &mut Inventory, n: u32) {
    inv.stock += n;
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn take_and_restock() {
        let mut i = Inventory { stock: 1 };
        assert!(i.take(1));
        assert!(!i.take(1));
        restock(&mut i, 2);
        assert_eq!(i.stock, 2);
    }
}
